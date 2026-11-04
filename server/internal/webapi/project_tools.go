package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"github.com/SatoriTours/kaiwu/server/internal/projecttools"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type toolRequest struct {
	OperationID    string `json:"operationId,omitempty"`
	Ensure         bool   `json:"ensure,omitempty"`
	ProjectID      string `json:"projectId"`
	TaskID         string `json:"taskId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	Path           string `json:"path,omitempty"`
	ID             string `json:"id,omitempty"`
}

func (h *handler) attachments(convID string) (projecttools.Attachments, error) {
	st := h.store.Snapshot()
	c, ok := st.Conversations[convID]
	if !ok {
		return projecttools.Attachments{}, workbench.Missing()
	}
	p, ok := st.Projects[c.ProjectID]
	if !ok {
		return projecttools.Attachments{}, workbench.Missing()
	}
	layout, err := projectstorage.ResolveProject(p.Storage(), h.cfg.DataDir)
	if err != nil {
		return projecttools.Attachments{}, err
	}
	if err := projectstorage.EnsureDir(layout.Attachments); err != nil {
		return projecttools.Attachments{}, err
	}
	return projecttools.Attachments{Dir: layout.Attachments}, nil
}
func (h *handler) toolScope(q toolRequest) (root, scope string, err error) {
	st := h.store.Snapshot()
	p, ok := st.Projects[q.ProjectID]
	if !ok {
		return "", "", workbench.Missing()
	}
	layout, e := projectstorage.ResolveProject(p.Storage(), h.cfg.DataDir)
	if e != nil {
		return "", "", e
	}
	if q.TaskID == "" {
		return p.Path, p.ID, nil
	}
	t, ok := st.Tasks[q.TaskID]
	if !ok || t.ProjectID != p.ID {
		return "", "", workbench.Missing()
	}
	x, ok := st.Executions[t.ExecutionID]
	if !ok || x.Workspace == "" {
		return "", "", workbench.Conflict("任务尚无独立工作区")
	}
	if p.StorageMode == "managed" && (!projectstorage.ValidDirectoryName(t.ID) || x.Workspace != filepath.Join(layout.Worktrees, t.ID, "tree") || x.TaskID != t.ID || x.ConversationID != t.ConversationID) {
		return "", "", workbench.Conflict("任务工作区不属于当前项目")
	}
	// A task scope is derived exclusively from the stored execution, not a client path.
	return x.Workspace, p.ID + "/" + t.ID, nil
}
func (h *handler) toolLease(ctx context.Context, q toolRequest) (preview.Lease, error) {
	if h.cfg.AcquireToolLease == nil {
		return nil, workbench.Conflict("人工写入锁未装配，未操作")
	}
	return h.cfg.AcquireToolLease(ctx, q.ProjectID, q.TaskID)
}
func releaseFileLease(l preview.Lease) {
	if n, ok := l.(interface{ NoProcess() }); ok {
		n.NoProcess()
	}
	_ = l.Release(true)
}
func (h *handler) normalizeAttachments(c *workbench.Command) error {
	if len(c.Attachments) == 0 {
		return nil
	}
	if c.Type != "send_message" || len(c.Attachments) > 8 || h.cfg.DataDir == "" {
		return workbench.Invalid("每条消息最多 8 个附件，仅普通消息支持附件")
	}
	conv, ok := h.store.Snapshot().Conversations[c.ConversationID]
	if !ok || conv.ProjectID != c.ProjectID {
		return workbench.Missing()
	}
	attachments, e := h.attachments(c.ConversationID)
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	total := 0
	for i, a := range c.Attachments {
		_, meta, e := attachments.Load(c.ConversationID, a.ID)
		if e != nil {
			return workbench.Invalid("附件不存在或不属于当前会话")
		}
		if seen[a.ID] {
			return workbench.Invalid("附件重复")
		}
		seen[a.ID] = true
		total += meta.Size
		c.Attachments[i] = meta
	}
	if total > 24<<20 {
		return workbench.Invalid("每条消息附件合计不能超过 24 MiB")
	}
	return nil
}
func (h *handler) projectTools(w http.ResponseWriter, r *http.Request) {
	if h.cfg.DataDir == "" {
		fail(w, 503, "tools_unavailable", "文件与终端服务尚未装配")
		return
	}
	path := r.URL.Path
	if path == "/api/attachments" || strings.HasPrefix(path, "/api/attachments/") {
		h.attachmentHTTP(w, r)
		return
	}
	q := toolRequest{ProjectID: r.URL.Query().Get("projectId"), TaskID: r.URL.Query().Get("taskId"), Path: r.URL.Query().Get("path"), ID: r.URL.Query().Get("id")}
	if r.Method == "POST" && path != "/api/tools/upload" {
		if !decode(w, r, &q) {
			return
		}
	}
	root, scope, e := h.toolScope(q)
	if e != nil {
		h.failure(w, e)
		return
	}
	if strings.HasPrefix(path, "/api/tools/terminal/") {
		switch {
		case path == "/api/tools/terminal/resolve" && r.Method == "POST":
			if q.OperationID == "" || len(q.OperationID) > 100 {
				h.failure(w, workbench.Invalid("终端操作ID无效"))
				return
			}
			result, known := h.terminals.Lookup(scope, q.OperationID)
			respond(w, 200, map[string]any{"known": known, "terminal": result})
		case path == "/api/tools/terminal/list" && r.Method == "POST":
			respond(w, 200, h.terminals.List(scope))
		case path == "/api/tools/terminal/start" && r.Method == "POST":
			t, e := h.terminals.Open(scope, root, q.OperationID, q.Ensure, func() (preview.Lease, error) {
				if h.cfg.AcquireTerminalLease != nil {
					return h.cfg.AcquireTerminalLease(r.Context(), q.ProjectID, q.TaskID)
				}
				return h.toolLease(r.Context(), q)
			})
			if e != nil {
				h.failure(w, workbench.Conflict(e.Error()))
				return
			}
			respond(w, 200, t)
		case (path == "/api/tools/terminal/stop" && r.Method == "POST") || (path == "/api/tools/terminal/socket" && r.Method == "GET"):
			t, e := h.terminals.Read(q.ID)
			if e != nil || t.Scope != scope {
				h.failure(w, workbench.Missing())
				return
			}
			if path == "/api/tools/terminal/stop" {
				if e = h.terminals.Stop(q.ID); e != nil {
					h.failure(w, workbench.Conflict(e.Error()))
					return
				}
				respond(w, 200, map[string]bool{"stopped": true})
			} else {
				h.terminalSocket(w, r, q.ID)
			}
		default:
			fail(w, 404, "not_found", "终端操作不存在")
		}
		return
	}
	f, e := projecttools.Open(root)
	if e != nil {
		h.failure(w, workbench.Conflict("工作区不可用"))
		return
	}
	defer f.Close()
	if q.Path == "" {
		q.Path = "."
	}
	switch {
	case path == "/api/tools/files" && r.Method == "GET":
		entries, e := f.List(q.Path)
		if e != nil {
			h.failure(w, workbench.Invalid(e.Error()))
			return
		}
		respond(w, 200, map[string]any{"entries": entries, "root": root})
	case path == "/api/tools/file" && r.Method == "GET":
		b, e := f.Read(q.Path)
		if e != nil {
			h.failure(w, workbench.Invalid(e.Error()))
			return
		}
		serveToolFile(w, b, filepath.Base(q.Path), false)
	case path == "/api/tools/mkdir" && r.Method == "POST":
		l, e := h.toolLease(r.Context(), q)
		if e != nil {
			h.failure(w, e)
			return
		}
		defer releaseFileLease(l)
		if e = f.Mkdir(q.Path); e != nil {
			h.failure(w, workbench.Invalid("无法新建目录，路径无效或已经存在"))
			return
		}
		respond(w, 200, map[string]bool{"created": true})
	case path == "/api/tools/upload" && r.Method == "POST":
		l, e := h.toolLease(r.Context(), q)
		if e != nil {
			h.failure(w, e)
			return
		}
		defer releaseFileLease(l)
		upload(w, r, func(name string, body io.Reader) error {
			if !projecttools.SafeName(name) {
				return workbench.Invalid("文件名无效")
			}
			target := name
			if q.Path != "." {
				target = q.Path + "/" + name
			}
			return f.Upload(target, body)
		})
	case path == "/api/tools/attach" && r.Method == "POST":
		c, ok := h.store.Snapshot().Conversations[q.ConversationID]
		if !ok || c.ProjectID != q.ProjectID {
			h.failure(w, workbench.Missing())
			return
		}
		b, e := f.Read(q.Path)
		if e != nil {
			h.failure(w, workbench.Invalid(e.Error()))
			return
		}
		attachments, e := h.attachments(c.ID)
		if e != nil {
			h.failure(w, e)
			return
		}
		a, e := attachments.Save(c.ID, filepath.Base(q.Path), strings.NewReader(string(b)))
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, a)
	default:
		fail(w, 404, "not_found", "文件操作不存在")
	}
}
func upload(w http.ResponseWriter, r *http.Request, save func(string, io.Reader) error) {
	r.Body = http.MaxBytesReader(w, r.Body, projecttools.MaxFile+(64<<10))
	reader, e := r.MultipartReader()
	if e != nil {
		fail(w, 400, "invalid_upload", "请上传单个文件")
		return
	}
	part, e := reader.NextPart()
	if e != nil || part.FormName() != "file" || part.FileName() == "" {
		fail(w, 400, "invalid_upload", "附件内容无效")
		return
	}
	if e = save(part.FileName(), part); e != nil {
		fail(w, 400, "invalid_upload", e.Error())
		return
	}
	respond(w, 200, map[string]bool{"uploaded": true})
}
func (h *handler) attachmentHTTP(w http.ResponseWriter, r *http.Request) {
	conv := r.URL.Query().Get("conversationId")
	if _, ok := h.store.Snapshot().Conversations[conv]; !ok {
		h.failure(w, workbench.Missing())
		return
	}
	attachments, e := h.attachments(conv)
	if e != nil {
		h.failure(w, e)
		return
	}
	if r.URL.Path == "/api/attachments" && r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, projecttools.MaxFile+(64<<10))
		mr, e := r.MultipartReader()
		if e != nil {
			fail(w, 400, "invalid_upload", "请上传单个文件")
			return
		}
		part, e := mr.NextPart()
		if e != nil || part.FormName() != "file" || !projecttools.SafeName(part.FileName()) {
			fail(w, 400, "invalid_upload", "附件无效")
			return
		}
		a, e := attachments.Save(conv, part.FileName(), part)
		if e != nil {
			h.failure(w, workbench.Invalid(e.Error()))
			return
		}
		respond(w, 200, a)
		return
	}
	if r.Method != "GET" {
		fail(w, 405, "method_not_allowed", "不支持此附件操作")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/attachments/")
	b, a, e := attachments.Load(conv, id)
	if e != nil {
		h.failure(w, workbench.Missing())
		return
	}
	serveToolFile(w, b, a.Name, r.URL.Query().Get("inline") == "1" && projecttools.ImageMIME(a.MIME))
}
func serveToolFile(w http.ResponseWriter, b []byte, name string, inline bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	disposition := "attachment"
	if inline {
		disposition = "inline"
		w.Header().Set("Content-Type", http.DetectContentType(b))
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	_, _ = w.Write(b)
}
func (h *handler) terminalSocket(w http.ResponseWriter, r *http.Request, id string) {
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
		fail(w, 403, "csrf", "需要同源终端连接")
		return
	}
	conn, e := websocket.Accept(w, r, nil)
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8192)
	ctx, cancel := context.WithCancel(h.cfg.Context)
	defer cancel()
	stream := r.URL.Query().Get("v") == "2"
	acks := make(chan uint64, 1)

	// Neither WebSocket closure nor changing project owns the shell.
	go func() {
		defer cancel()
		for {
			_, raw, e := conn.Read(ctx)
			if e != nil {
				return
			}
			var input struct {
				Input  string `json:"input"`
				Resize *struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				} `json:"resize"`
				Ack *uint64 `json:"ack"`
			}
			if json.Unmarshal(raw, &input) != nil || len(input.Input) > 4096 {
				return
			}
			if !h.authenticated(r) {
				return
			}
			if input.Ack != nil && stream {
				select {
				case acks <- *input.Ack:
				default:
					return
				}
			}
			if input.Resize != nil {
				if e = h.terminals.Resize(id, input.Resize.Cols, input.Resize.Rows); e != nil {
					return
				}
			}
			if input.Input != "" {
				if e = h.terminals.Input(id, input.Input); e != nil {
					return
				}
			}
		}
	}()
	cursor := int64(-1)
	sequence := uint64(0)
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		if !h.authenticated(r) {
			return
		}
		changed, e := h.terminals.Changed(id)
		if e != nil {
			return
		}
		t, e := h.terminals.ReadSince(id, cursor)
		if e != nil {
			return
		}
		var data []byte
		if stream {
			sequence++
			// Base64 preserves UTF-8 split across PTY reads, unlike JSON strings.
			data, _ = json.Marshal(struct {
				Data      []byte `json:"data"`
				Sequence  uint64 `json:"sequence"`
				Reset     bool   `json:"reset"`
				Running   bool   `json:"running"`
				Cols      int    `json:"cols"`
				Rows      int    `json:"rows"`
				Truncated bool   `json:"truncated"`
			}{[]byte(t.Output), sequence, t.Reset, t.Running, t.Cols, t.Rows, t.Reset && t.Offset > int64(len(t.Output))})
		} else {
			data, _ = json.Marshal(t)
		}
		cursor = t.Offset
		write, done := context.WithTimeout(ctx, 5*time.Second)
		e = conn.Write(write, websocket.MessageText, data)
		done()
		if e != nil {
			return
		}
		if stream {
			// Keep at most one bounded frame in the browser's write queue.
			// A slow/hidden client cannot accumulate unlimited output. The shell
			// remains alive and its bounded history can be reattached explicitly.
			wait, stop := context.WithTimeout(ctx, 15*time.Second)
			select {
			case ack := <-acks:
				stop()
				if ack != sequence {
					return
				}
			case <-wait.Done():
				stop()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		case <-tick.C:
			if !h.authenticated(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "登录已失效")
				return
			}
		}
	}
}
