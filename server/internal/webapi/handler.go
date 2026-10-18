// Package webapi exposes authenticated local workbench resources.
package webapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
)

type Commander interface {
	Send(string, string) error
	Resume(string) error
	// AnswerInteraction delivers one stored native answer to the original pi
	// extension wait. Ordinary answers use Send with the durable message id.
	AnswerInteraction(conversationID, interactionID string) error
}
type Config struct {
	Previews *preview.Manager
	Models   interface {
		List(context.Context) ([]pimodel.Model, error)
	}
	Git            GitCommander
	Delivery       DeliveryCommander
	Context        context.Context
	Token, Host    string
	Assets         fs.FS
	Health         http.Handler
	AgentAvailable bool
}
type handler struct {
	cfg           Config
	store         *workbench.Store
	agent         Commander
	mu            sync.Mutex
	attempts      int
	attemptWindow time.Time
	socketWG      sync.WaitGroup
	socketClosed  bool
	cancel        context.CancelFunc
	streamID      string
}

func New(cfg Config, store *workbench.Store, agent Commander) http.Handler {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	owned, cancel := context.WithCancel(cfg.Context)
	cfg.Context = owned
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return &handler{cfg: cfg, store: store, agent: agent, cancel: cancel, streamID: hex.EncodeToString(b)}
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (h *handler) failure(w http.ResponseWriter, err error) {
	var e *workbench.Error
	if errors.As(err, &e) {
		status := 400
		if e.Code == "not_found" {
			status = 404
		}
		if e.Code == "conflict" {
			status = 409
		}
		fail(w, status, e.Code, e.Message)
		return
	}
	fail(w, 500, "internal_error", "操作未完成，请检查服务状态；未自动重试")
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		fail(w, 415, "invalid_content_type", "请发送 JSON")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_json", "JSON 内容无效或过大")
		return false
	}
	return true
}
func (h *handler) allowed(r *http.Request) bool {
	host := r.Host
	configured := h.cfg.Host
	colon := strings.LastIndex(configured, ":")
	if colon < 0 {
		return false
	}
	port := configured[colon:]
	if host != configured && host != "localhost"+port {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "http" || u.Host != host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return false
		}
	}
	return true
}
func (h *handler) authenticated(r *http.Request) bool {
	c, e := r.Cookie("kaiwu_session")
	if e != nil || len(c.Value) != 64 {
		return false
	}
	return h.store.ValidLogin(credentialHash(c.Value), credentialHash(h.cfg.Token))
}

func credentialHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if strings.HasPrefix(r.URL.Path, "/preview/") {
		h.previewGateway(w, r)
		return
	}
	if !h.allowed(r) {
		fail(w, 403, "forbidden_origin", "访问来源不受允许")
		return
	}
	if r.URL.Path == "/healthz" && h.cfg.Health != nil {
		h.cfg.Health.ServeHTTP(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		h.static(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		if r.Header.Get("Origin") == "" || r.Header.Get("X-Kaiwu-Client") != "web" {
			fail(w, 403, "csrf", "请求缺少同源验证")
			return
		}
	}
	if r.URL.Path == "/api/login" && r.Method == "POST" {
		h.login(w, r)
		return
	}
	if !h.authenticated(r) {
		fail(w, 401, "unauthorized", "请先登录本机工作台")
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/previews/"):
		h.taskPreview(w, r)
	case r.URL.Path == "/api/models" && r.Method == "GET":
		if h.cfg.Models == nil {
			fail(w, 503, "models_unavailable", "服务未找到 pi，请检查服务 PATH 或 KAIWU_PI_PROGRAM")
			return
		}
		if r.URL.Query().Get("refresh") == "1" {
			if catalog, ok := h.cfg.Models.(interface{ InvalidateCache() }); ok {
				catalog.InvalidateCache()
			}
		}
		models, err := h.cfg.Models.List(r.Context())
		if err != nil {
			fail(w, 503, "models_unavailable", err.Error())
			return
		}
		respond(w, 200, map[string]any{"models": models})
	case strings.HasPrefix(r.URL.Path, "/api/git/"):
		h.git(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/delivery/"):
		h.delivery(w, r)
	case r.URL.Path == "/api/session" && r.Method == "GET":
		respond(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/snapshot" && r.Method == "GET":
		respond(w, 200, map[string]any{"state": h.store.Snapshot(), "capabilities": map[string]bool{"git": h.cfg.Git != nil, "merge": h.cfg.Git != nil && pirpc.ProcessGroupsSupported(), "agent": h.cfg.AgentAvailable, "execution": h.cfg.AgentAvailable && pirpc.ProcessGroupsSupported(), "interactions": true}})
	case r.URL.Path == "/api/logout" && r.Method == "POST":
		c, _ := r.Cookie("kaiwu_session")
		if err := h.store.RevokeLogin(credentialHash(c.Value)); err != nil {
			h.failure(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "kaiwu_session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		respond(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/commands" && r.Method == "POST":
		h.command(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/operations/") && r.Method == "GET":
		receipt, e := h.store.Operation(strings.TrimPrefix(r.URL.Path, "/api/operations/"))
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, receipt)
	case r.URL.Path == "/api/socket" && r.Method == "GET":
		h.socket(w, r)
	case r.URL.Path == "/api/events" && r.Method == "GET":
		h.events(w, r)
	default:
		fail(w, 404, "not_found", "API 不存在")
	}
}
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if time.Since(h.attemptWindow) >= time.Minute {
		h.attempts = 0
		h.attemptWindow = time.Now()
	}
	if h.attempts >= 10 {
		h.mu.Unlock()
		fail(w, 429, "rate_limited", "登录尝试过多，请一分钟后重试")
		return
	}
	h.attempts++
	h.mu.Unlock()
	var input struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &input) {
		return
	}
	a, b := sha256.Sum256([]byte(input.Token)), sha256.Sum256([]byte(h.cfg.Token))
	if len(h.cfg.Token) < 32 || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		fail(w, 401, "unauthorized", "令牌无效")
		return
	}
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		h.failure(w, e)
		return
	}
	key := hex.EncodeToString(raw)
	const lifetime = 30 * 24 * time.Hour
	expires := time.Now().Add(lifetime)
	if err := h.store.SaveLogin(credentialHash(key), credentialHash(h.cfg.Token), expires); err != nil {
		h.failure(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "kaiwu_session", Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(lifetime.Seconds()), Expires: expires})
	respond(w, 200, map[string]bool{"ok": true})
}
func (h *handler) command(w http.ResponseWriter, r *http.Request) {
	var c workbench.Command
	if !decode(w, r, &c) {
		return
	}
	if !interactionAnswerGuard(w, c) {
		return
	}
	if c.Type == "update_project_git" {
		if h.cfg.Git == nil {
			fail(w, 503, "git_unavailable", "Git 集成不可用")
			return
		}
		receipt, e := h.cfg.Git.UpdateProjectGit(r.Context(), c)
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, receipt)
		return
	}
	var prepare func() error
	if c.Type == "set_default_profile" || c.Type == "set_conversation_profile" {
		if h.cfg.Models == nil {
			fail(w, 503, "models_unavailable", "服务未找到 pi，无法选择模型")
			return
		}
		models, err := h.cfg.Models.List(r.Context())
		if err != nil {
			fail(w, 503, "models_unavailable", err.Error())
			return
		}
		found, reasoning := false, false
		for _, model := range models {
			if model.Provider == c.Provider && model.ID == c.Model {
				found, reasoning = true, model.Reasoning
				break
			}
		}
		if !found {
			fail(w, 400, "model_unavailable", "该模型不在 pi 当前可用列表，请重新检测")
			return
		}
		if !reasoning && c.Thinking != "" && c.Thinking != "off" {
			fail(w, 400, "invalid_thinking", "该模型不支持思考级别")
			return
		}
	}
	// set_next_profile applies to the next round boundary only: it never
	// interrupts a running request and never rewrites the project template.
	if c.Type == "set_next_profile" {
		if h.cfg.Models == nil {
			fail(w, 503, "models_unavailable", "服务未找到 pi，无法选择模型")
			return
		}
		models, err := h.cfg.Models.List(r.Context())
		if err != nil {
			fail(w, 503, "models_unavailable", err.Error())
			return
		}
		found, reasoning := false, false
		for _, model := range models {
			if model.Provider == c.Provider && model.ID == c.Model {
				found, reasoning = true, model.Reasoning
				break
			}
		}
		if !found {
			fail(w, 400, "model_unavailable", "该模型不在 pi 当前可用列表，请重新检测")
			return
		}
		if !reasoning && c.Thinking != "" && c.Thinking != "off" {
			fail(w, 400, "invalid_thinking", "该模型不支持思考级别")
			return
		}
		receipt, e := h.store.RequestNextProfile(c.OperationID, c.ConversationID, workbench.Profile{Provider: c.Provider, Model: c.Model, Thinking: c.Thinking})
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, receipt)
		return
	}
	if c.Type == "add_project" {
		canonical, e := canonicalProjectPath(c.Path)
		if e != nil {
			h.failure(w, e)
			return
		}
		c.Path = canonical
		c.RemoteURL = strings.TrimSpace(c.RemoteURL)
		prepare = func() error { return prepareProject(r.Context(), c) }
	}
	if c.Type == "send_message" && workbench.IsStartRequest(c.Text) {
		c.Type = "start_task"
		if c.TaskID == "" {
			c.TaskID = h.store.Snapshot().Conversations[c.ConversationID].TaskID
		}
	}
	if c.Type == "start_task" && c.TaskID == "" {
		h.failure(w, workbench.Invalid("请先选择一张任务卡片，再开始开发"))
		return
	}
	if (c.Type == "send_message" || c.Type == "resume" || c.Type == "start_task") && !h.cfg.AgentAvailable {
		fail(w, 503, "agent_unavailable", "服务未找到 pi，请检查服务 PATH 或设置 KAIWU_PI_PROGRAM 后重启；输入已保留")
		return
	}
	if c.Type == "start_task" && h.cfg.Delivery == nil {
		policy, err := h.store.DeliveryPolicy(c.ProjectID)
		if err != nil {
			h.failure(w, err)
			return
		}
		if policy.Enabled {
			fail(w, 503, "delivery_unavailable", "自动交付未装配，请检查服务器凭据；任务尚未启动")
			return
		}
	}
	if c.Type == "start_task" && !pirpc.ProcessGroupsSupported() {
		fail(w, 503, "execution_unavailable", "当前平台尚不支持开发执行")
		return
	}
	if c.Type == "send_message" || c.Type == "resume" {
		conversation, ok := h.store.Snapshot().Conversations[c.ConversationID]
		if ok && (conversation.Profile.Provider == "" || conversation.Profile.Model == "") {
			fail(w, 503, "profile_unavailable", "请先点击聊天输入框下方的“选择模型”；输入已保留")
			return
		}
	}
	receipt, e := h.store.PreparedCommand(c, prepare)
	if e != nil {
		h.failure(w, e)
		return
	}
	if !receipt.Replay && h.agent != nil {
		switch receipt.Type {
		case "send_message", "start_task":
			if receipt.DeliveryID != "" {
				// The worker owns dispatch and its durable intent. The HTTP
				// path must not launch a second, untracked request.
				h.cfg.Delivery.Wake()
			} else {
				e = h.agent.Send(receipt.ConversationID, receipt.CreatedID)
			}
		case "resume":
			e = h.agent.Resume(receipt.ConversationID)
		}
		if e != nil {
			_ = h.store.Update(func(st *workbench.State) error {
				v := st.Conversations[receipt.ConversationID]
				v.RunState = "failed"
				v.Error = "会话未启动，请检查配置后再次发送"
				for i := range v.Messages {
					if v.Messages[i].ID == receipt.CreatedID {
						v.Messages[i].Delivery = "failed"
					}
				}
				st.Conversations[v.ID] = v
				if t, ok := st.Tasks[v.TaskID]; ok && t.ExecutionID != "" {
					x := st.Executions[t.ExecutionID]
					x.Phase = "failed"
					x.Error = "会话未启动，进程容量或配置不可用；请明确重试"
					t.Status = "failed"
					st.Executions[x.ID] = x
					st.Tasks[t.ID] = t
				}
				return nil
			})
		}
	}
	if workbench.IsInteractionCommand(c.Type) {
		if !receipt.Replay {
			if notice := h.dispatchInteraction(receipt); notice != "" {
				receipt.Notice = notice
			}
		}
		respond(w, 200, h.interactionEnvelope(receipt))
		return
	}
	respond(w, 200, receipt)
}
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "" {
		fail(w, 403, "forbidden_origin", "事件连接需要同源验证")
		return
	}
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024)
	owned, cancelOwned := context.WithCancel(r.Context())
	defer cancelOwned()
	stop := context.AfterFunc(h.cfg.Context, cancelOwned)
	defer stop()
	ctx := conn.CloseRead(owned)
	events, unsubscribe := h.store.Subscribe()
	defer unsubscribe()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			ping, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = conn.Ping(ping)
			cancel()
			if e != nil {
				return
			}
		case revision := <-events:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			body, _ := json.Marshal(map[string]uint64{"revision": revision})
			write, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = conn.Write(write, websocket.MessageText, body)
			cancel()
			if e != nil {
				return
			}
		}
	}
}
func (h *handler) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		fail(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	if h.cfg.Assets == nil {
		fail(w, 503, "web_not_built", "请先构建 apps/web")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" || strings.HasPrefix(name, "projects/") {
		name = "index.html"
	}
	body, e := fs.ReadFile(h.cfg.Assets, name)
	if e != nil {
		if name == "index.html" {
			fail(w, 503, "web_not_built", "请先构建 apps/web")
			return
		}
		fail(w, 404, "not_found", "页面或静态资源不存在")
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public,max-age=31536000,immutable")
	}
	if r.Method != "HEAD" {
		_, _ = w.Write(body)
	}
}
