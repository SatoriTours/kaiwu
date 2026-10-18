package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type socketRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Body   json.RawMessage `json:"body,omitempty"`
}
type socketReply struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
}
type rpcResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *rpcResponse) Header() http.Header    { return w.header }
func (w *rpcResponse) WriteHeader(status int) { w.status = status }
func (w *rpcResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	if w.body.Len()+len(b) > 32<<20 {
		return 0, io.ErrShortBuffer
	}
	return w.body.Write(b)
}
func decodeSocketBody(raw json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func socketFailure(id string, status int, message string) socketReply {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "socket_error", "message": message}})
	return socketReply{Type: "reply", ID: id, Status: status, Data: b}
}
func (h *handler) socketCall(r *http.Request, input socketRequest) socketReply {
	method := "POST"
	path := ""
	query := ""
	switch input.Method {
	case "commands", "git/refresh", "git/preview", "git/merge", "previews/status", "previews/start", "previews/stop", "previews/open",
		"delivery/policy", "delivery/preflight", "delivery/accept", "delivery/control", "delivery/status", "delivery/steps":
		path = "/api/" + input.Method
	case "models", "models?refresh=1":
		method = "GET"
		path = "/api/models"
		if input.Method == "models?refresh=1" {
			query = "refresh=1"
		}
	default:
		for _, prefix := range []string{"operations/", "git/operations/"} {
			if strings.HasPrefix(input.Method, prefix) {
				id := strings.TrimPrefix(input.Method, prefix)
				if len(id) > 0 && len(id) <= 100 && !strings.ContainsAny(id, "/\\?#%\x00") {
					method = "GET"
					path = "/api/" + input.Method
				}
			}
		}
	}
	if path == "" {
		return socketFailure(input.ID, 400, "不支持的实时请求")
	}
	req := r.Clone(h.cfg.Context)
	req.Method = method
	req.URL = &url.URL{Path: path, RawQuery: query}
	req.Body = io.NopCloser(bytes.NewReader(input.Body))
	req.ContentLength = int64(len(input.Body))
	req.Header = req.Header.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kaiwu-Client", "web")
	out := &rpcResponse{header: make(http.Header)}
	h.ServeHTTP(out, req)
	if out.status == 0 {
		out.status = 200
	}
	if !json.Valid(out.body.Bytes()) {
		return socketFailure(input.ID, 500, "实时响应未完整生成，请查询原操作回执")
	}
	return socketReply{Type: "reply", ID: input.ID, Status: out.status, Data: append(json.RawMessage{}, out.body.Bytes()...)}
}
func (h *handler) Close() error {
	h.mu.Lock()
	h.socketClosed = true
	h.cancel()
	h.mu.Unlock()
	h.socketWG.Wait()
	return nil
}
func (h *handler) socket(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.socketClosed {
		h.mu.Unlock()
		fail(w, 503, "closing", "服务正在关闭")
		return
	}
	h.socketWG.Add(1)
	h.mu.Unlock()
	defer h.socketWG.Done()
	if r.Header.Get("Origin") == "" {
		fail(w, 403, "forbidden_origin", "实时连接需要同源验证")
		return
	}
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(h.cfg.Context, cancel)
	defer stop()
	requests := make(chan socketRequest, 8)
	jobs := make(chan socketRequest, 8)
	replies := make(chan socketReply, 8)
	go func() {
		defer cancel()
		for {
			kind, b, e := conn.Read(ctx)
			if e != nil {
				return
			}
			var req socketRequest
			d := json.NewDecoder(bytes.NewReader(b))
			d.DisallowUnknownFields()
			if kind != websocket.MessageText || d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || len(req.ID) == 0 || len(req.ID) > 100 {
				return
			}
			select {
			case requests <- req:
			case <-ctx.Done():
				return
			}
		}
	}()
	workerDone := make(chan struct{})
	defer func() { cancel(); <-workerDone }()
	go func() {
		defer close(workerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case req := <-jobs:
				if ctx.Err() != nil {
					return
				}
				reply := h.socketCall(r, req)
				select {
				case replies <- reply:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	events, unsubscribe := h.store.Subscribe()
	defer unsubscribe()
	write := func(value any) error {
		body, e := json.Marshal(value)
		if e != nil || len(body) > 32<<20 {
			return io.ErrShortBuffer
		}
		timeout, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return conn.Write(timeout, websocket.MessageText, body)
	}
	cursor := uint64(0)
	initialized := false
	sync := func(after uint64) error {
		deltas, reset := h.store.SyncSince(after)
		if after == 0 {
			snapshot := h.store.Snapshot()
			reset = &snapshot
			deltas = nil
		}
		if reset != nil {
			if e := writeSnapshot(map[string]any{"type": "snapshot", "streamId": h.streamID, "state": reset, "capabilities": map[string]bool{"agent": h.cfg.AgentAvailable, "git": h.cfg.Git != nil}}, write); e != nil {
				return e
			}
			cursor = reset.Revision
		} else {
			for _, delta := range deltas {
				if e := write(map[string]any{"type": "patch", "streamId": h.streamID, "after": delta.After, "revision": delta.Revision, "ops": delta.Ops}); e != nil {
					return e
				}
				cursor = delta.Revision
			}
			if len(deltas) == 0 {
				cursor = after
			}
		}
		initialized = true
		return nil
	}
	// Only observation follows connection visibility; registered business work uses owner context.
	watchChanges := make(chan string, 1)
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); h.watchGit(ctx, watchChanges) }()
	defer func() { cancel(); <-watchDone }()
	setWatch := func(id string) {
		select {
		case <-watchChanges:
		default:
		}
		select {
		case watchChanges <- id:
		case <-ctx.Done():
		}
	}
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
			ping, done := context.WithTimeout(ctx, 5*time.Second)
			e := conn.Ping(ping)
			done()
			if e != nil {
				return
			}
		case <-events:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			if initialized {
				if sync(cursor) != nil {
					return
				}
			}
		case reply := <-replies:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			if write(reply) != nil {
				return
			}
		case input := <-requests:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			switch input.Method {
			case "sync":
				var body struct {
					After uint64 `json:"after"`
				}
				if decodeSocketBody(input.Body, &body) != nil {
					if write(socketFailure(input.ID, 400, "同步游标无效")) != nil {
						return
					}
					continue
				}
				if sync(body.After) != nil {
					return
				}
				if write(socketReply{Type: "reply", ID: input.ID, Status: 200, Data: json.RawMessage(`{"ok":true}`)}) != nil {
					return
				}
			case "watch":
				var body struct {
					ProjectID string `json:"projectId"`
					Visible   bool   `json:"visible"`
				}
				if decodeSocketBody(input.Body, &body) != nil {
					if write(socketFailure(input.ID, 400, "订阅无效")) != nil {
						return
					}
					continue
				}
				id := ""
				if body.Visible && body.ProjectID != "" {
					if _, ok := h.store.Snapshot().Projects[body.ProjectID]; !ok {
						if write(socketFailure(input.ID, 404, "项目不存在")) != nil {
							return
						}
						continue
					}
					id = body.ProjectID
				}
				setWatch(id)
				if write(socketReply{Type: "reply", ID: input.ID, Status: 200, Data: json.RawMessage(`{"ok":true}`)}) != nil {
					return
				}
			default:
				select {
				case jobs <- input:
				default:
					if write(socketFailure(input.ID, 409, "实时请求繁忙，请稍后重试")) != nil {
						return
					}
				}
			}
		}
	}
}
func (h *handler) watchGit(ctx context.Context, changes <-chan string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	id := ""
	var cancel context.CancelFunc
	var done <-chan struct{}
	pending := false
	defer func() {
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}
	}()
	launch := func() {
		if h.cfg.Git == nil || id == "" || done != nil {
			return
		}
		checkCtx, stop := context.WithCancel(ctx)
		cancel = stop
		finished := make(chan struct{})
		done = finished
		target := id
		go func() { defer close(finished); _ = h.cfg.Git.RefreshGit(checkCtx, target) }()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case target := <-changes:
			if cancel != nil {
				cancel()
			}
			id = target
			pending = done != nil
			if done == nil {
				launch()
			}
		case <-done:
			done = nil
			cancel = nil
			if pending {
				pending = false
				launch()
			}

		case <-ticker.C:
			launch()
		}
	}
}
