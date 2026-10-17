package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type Request struct {
	ManifestID  string `json:"manifestId,omitempty"`
	ProjectID   string `json:"projectId"`
	TaskID      string `json:"taskId"`
	RoundID     string `json:"roundId"`
	OperationID string `json:"operationId,omitempty"`
}
type TaskPreview struct {
	ManifestID string   `json:"manifestId,omitempty"`
	TaskID     string   `json:"taskId"`
	RoundID    string   `json:"roundId"`
	State      string   `json:"state"`
	Message    string   `json:"message"`
	Command    []string `json:"command,omitempty"`
	Path       string   `json:"path,omitempty"`
	URL        string   `json:"url,omitempty"`
	Logs       string   `json:"logs,omitempty"`
}

// Lease is the same execution lock used by pi and Git integration. Its open
// file description is inherited by the child, so a host crash cannot unlock it.
type Lease interface {
	File() *os.File
	Attempt() error
	Started(int) error
	Release(bool) error
}
type Config struct {
	DataDir string
	Reserve func(context.Context, string) (Lease, error)
}
type entry struct {
	req     Request
	tree    string
	hash    [32]byte
	view    TaskPreview
	cmd     *exec.Cmd
	process *processGroup
	lease   Lease
	port    int
	done    chan struct{}
	exitErr error
	logs    tail
}
type capability struct {
	entry   *entry
	login   string
	expires time.Time
}
type operation struct {
	method string
	req    Request
}
type Manager struct {
	mu             sync.Mutex
	ctx            context.Context
	store          *workbench.Store
	cfg            Config
	entries        map[string]*entry
	tokens         map[string]*capability
	operations     map[string]operation
	closed         bool
	startupTimeout time.Duration
	workers        sync.WaitGroup
}

func New(ctx context.Context, s *workbench.Store, cfg Config) *Manager {
	return &Manager{ctx: ctx, store: s, cfg: cfg, entries: map[string]*entry{}, tokens: map[string]*capability{}, operations: map[string]operation{}, startupTimeout: 10 * time.Second}
}

func (m *Manager) target(req Request) (string, error) {
	st := m.store.Snapshot()
	t, ok := st.Tasks[req.TaskID]
	p, pok := st.Projects[req.ProjectID]
	c, cok := st.Conversations[t.ConversationID]
	x, xok := st.Executions[t.ExecutionID]
	if !ok || !pok || !cok || t.ProjectID != p.ID || c.ProjectID != p.ID || c.TaskID != t.ID {
		return "", workbench.Missing()
	}
	if c.RunState == "starting" || c.RunState == "running" || workbench.TaskMerging(st, t.ID) {
		return "", workbench.Conflict("任务开发或合并中，暂不能预览")
	}
	if !xok || x.Workspace == "" {
		return "", nil
	}
	if x.TaskID != t.ID || x.ConversationID != c.ID {
		return "", workbench.Conflict("执行身份不匹配")
	}
	if x.Phase != "awaiting_review" && x.Phase != "confirmed" {
		return "", workbench.Conflict("执行尚未结束，不能把旧轮次当作当前效果")
	}
	if len(c.Rounds) == 0 {
		if req.RoundID != "legacy:"+x.ID+":"+x.FinishedAt || x.FinishedAt == "" || (x.Phase != "awaiting_review" && x.Phase != "confirmed") {
			return "", workbench.Conflict("没有最新已结束轮次可供预览")
		}
	} else {
		r := c.Rounds[len(c.Rounds)-1]
		if req.RoundID != r.ID || (r.Phase != "finished" && r.Phase != "confirmed") {
			return "", workbench.Conflict("仅能预览最新已结束轮次；历史效果未存档")
		}
	}
	data, e := filepath.Abs(m.cfg.DataDir)
	if e != nil {
		return "", e
	}
	data, e = filepath.EvalSymlinks(data)
	if e != nil {
		return "", e
	}
	expected := filepath.Join(data, "worktrees", t.ID, "tree")
	if x.Workspace != expected || x.Branch != "kaiwu/task-"+t.ID {
		return "", workbench.Conflict("预览必须使用原任务独立工作区")
	}
	source, e := filepath.EvalSymlinks(p.Path)
	if e != nil || source != p.Path {
		return "", workbench.Conflict("原项目路径已改变")
	}
	var owner struct {
		TaskID, Source, Branch, BaseSHA string
		Ready                           bool
	}
	b, e := os.ReadFile(filepath.Join(filepath.Dir(expected), "owner.json"))
	if e != nil || json.Unmarshal(b, &owner) != nil || !owner.Ready || owner.TaskID != t.ID || owner.Source != source || owner.Branch != x.Branch || owner.BaseSHA != x.BaseSHA {
		return "", workbench.Conflict("任务工作区归属记录不匹配")
	}
	ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
	defer cancel()
	if e = gitops.ValidateWorktree(ctx, source, expected, x.Branch); e != nil {
		return "", workbench.Conflict(e.Error())
	}
	return expected, nil
}
func base(req Request, state, message string) TaskPreview {
	return TaskPreview{TaskID: req.TaskID, RoundID: req.RoundID, State: state, Message: message}
}
func (m *Manager) describe(req Request) (TaskPreview, string, [32]byte, error) {
	var hash [32]byte
	tree, e := m.target(req)
	if e != nil {
		return TaskPreview{}, "", hash, e
	}
	if tree == "" {
		return base(req, "unavailable", "暂无网页效果：任务还没有独立工作区；已有成果仍可查看"), tree, hash, nil
	}
	v, h, e := readManifest(tree)
	if os.IsNotExist(e) {
		return base(req, "unavailable", "暂无网页效果：工作区没有 kaiwu-preview.json；已有运行结果仍可查看"), tree, hash, nil
	}
	if e != nil {
		return base(req, "unavailable", e.Error()), tree, hash, nil
	}
	view := base(req, "stopped", "仅支持只读 HTTP GET/HEAD；不支持应用登录、WebSocket/HMR 或跨源资源，根绝对资源路径可能不兼容。点击启动才执行命令。")
	view.Command = append([]string{}, v.Command...)
	view.Path = v.Path
	view.ManifestID = hex.EncodeToString(h[:])
	return view, tree, h, nil
}
func (m *Manager) snapshot(e *entry) TaskPreview {
	v := e.view
	v.Command = append([]string{}, v.Command...)
	v.Logs = e.logs.String()
	return v
}
func (m *Manager) stateLocked(req Request) (TaskPreview, error) {
	v, tree, hash, err := m.describe(req)
	if err != nil {
		return v, err
	}
	if e := m.entries[req.TaskID]; e != nil {
		if e.req.RoundID != req.RoundID || e.tree != tree || e.hash != hash {
			if err := m.stopEntry(e, "预览配置或轮次已变化，请重新启动"); err != nil {
				return TaskPreview{}, err
			}
			delete(m.entries, req.TaskID)
			return v, nil
		}
		select {
		case <-e.done:
			if e.view.State == "running" || e.view.State == "starting" {
				_ = m.stopEntry(e, "预览进程已退出，请查看诊断后重试")
				e.view.State = "failed"
			}
		default:
		}
		if e.view.State == "running" {
			if err := ownedPort(e.port, e.cmd.Process.Pid); err != nil {
				_ = m.stopEntry(e, "预览端口归属已变化，已停止代理")
				e.view.State = "failed"
			}
		}
		return m.snapshot(e), nil
	}
	return v, nil
}
func (m *Manager) Status(req Request) (TaskPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked(req)
}
func (m *Manager) operation(req Request, method string) error {
	if len(req.OperationID) < 1 || len(req.OperationID) > 100 || strings.ContainsAny(req.OperationID, "\x00\r\n") {
		return workbench.Invalid("start/stop 需要有效 operationId")
	}
	if old, ok := m.operations[req.OperationID]; ok {
		if old.method != method || old.req != req {
			return workbench.Conflict("operationId 已用于不同预览操作")
		}
		return nil
	}
	if len(m.operations) >= 4096 {
		return workbench.Conflict("本次服务的预览操作已达上限，请重启服务后再操作")
	}
	m.operations[req.OperationID] = operation{method, req}
	return nil
}
func (m *Manager) Start(req Request) (TaskPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return TaskPreview{}, workbench.Conflict("预览服务已关闭")
	}
	// Bind explicit startup approval to exactly the manifest the client showed.
	// This read-only check precedes state cleanup, reservation, idle pi shutdown,
	// lease transfer and process execution. Never silently approve a newer file.
	displayed, _, _, err := m.describe(req)
	if err != nil {
		return displayed, err
	}
	if req.ManifestID == "" || displayed.ManifestID == "" || req.ManifestID != displayed.ManifestID {
		return TaskPreview{}, workbench.Conflict("预览配置已变化或尚未展示，请刷新并核对命令后重新启动")
	}
	_, replay := m.operations[req.OperationID]
	if e := m.operation(req, "start"); e != nil {
		return TaskPreview{}, e
	}
	v, err := m.stateLocked(req)
	if err != nil {
		return v, err
	}
	if replay || v.State == "running" || v.State == "starting" || v.State == "unavailable" {
		return v, nil
	}
	active := 0
	for _, e := range m.entries {
		if e.lease != nil {
			active++
		}
	}
	if active >= 4 {
		return v, workbench.Conflict("最多同时运行 4 个任务预览，请先停止一个")
	}
	tree, e := m.target(req)
	if e != nil {
		return v, e
	}
	manifest, hash, e := readManifest(tree)
	if e != nil {
		return v, workbench.Conflict("预览配置已变化，请刷新")
	}
	if req.ManifestID != hex.EncodeToString(hash[:]) {
		return TaskPreview{}, workbench.Conflict("预览配置已变化，请刷新并核对命令后重新启动")
	}
	var lease Lease
	if m.cfg.Reserve != nil {
		lease, e = m.cfg.Reserve(m.ctx, req.TaskID)
	} else {
		lease, e = acquireLock(tree)
	}
	if e != nil {
		return v, workbench.Conflict(e.Error())
	}
	safe := true
	defer func() {
		if lease != nil {
			_ = lease.Release(safe)
		}
	}()
	if _, e = m.target(req); e != nil {
		return v, e
	}
	_, fresh, e := readManifest(tree)
	if e != nil || fresh != hash {
		return v, workbench.Conflict("预览配置已变化，请刷新后启动")
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return v, e
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	argv := append([]string{}, manifest.Command...)
	for i := range argv {
		argv[i] = strings.ReplaceAll(argv[i], "{port}", strconv.Itoa(port))
	}
	en := &entry{req: req, tree: tree, hash: hash, view: v, port: port, lease: lease, done: make(chan struct{})}
	en.view.State = "starting"
	en.view.Command = argv
	cmd := exec.Command(argv[0], argv[1:]...)
	en.cmd = cmd
	en.process = &processGroup{cmd: cmd}
	cmd.Dir = tree
	cmd.Env = previewEnv()
	cmd.Stdout = &en.logs
	cmd.Stderr = &en.logs
	cmd.WaitDelay = time.Second
	setupProcess(cmd)
	cmd.ExtraFiles = []*os.File{lease.File()}
	if e = lease.Attempt(); e != nil {
		return v, e
	}
	if e = cmd.Start(); e != nil {
		en.view.State = "failed"
		en.view.Message = "预览命令启动失败：" + e.Error()
		cleanupErr := lease.Release(true)
		lease = nil
		en.lease = nil
		close(en.done)
		m.entries[req.TaskID] = en
		return m.snapshot(en), cleanupErr
	}
	safe = false
	m.entries[req.TaskID] = en
	lease = nil
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		en.exitErr = en.process.wait()
		close(en.done)
		// Exit owns its own cleanup; no browser connection or status polling is
		// required to release a stopped writer's lease and preview capacity.
		m.mu.Lock()
		defer m.mu.Unlock()
		if en.lease != nil {
			err := m.stopEntry(en, "预览进程已退出，请查看诊断后重试")
			en.view.State = "failed"
			if err != nil {
				en.view.Message = err.Error()
			}
		}
	}()
	if e = en.lease.Started(cmd.Process.Pid); e != nil {
		_ = m.stopEntry(en, "无法保存预览归属，已停止")
		en.view.State = "failed"
		return m.snapshot(en), nil
	}
	deadline := time.NewTimer(m.startupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			_ = m.stopEntry(en, "服务正在关闭")
			en.view.State = "failed"
			return m.snapshot(en), nil
		case <-en.done:
			_ = m.stopEntry(en, "预览进程在就绪前退出，请查看诊断后重试")
			en.view.State = "failed"
			return m.snapshot(en), nil
		case <-deadline.C:
			_ = m.stopEntry(en, "预览未在 10 秒内提供受控回环 HTTP 服务，请检查命令和日志")
			en.view.State = "failed"
			return m.snapshot(en), nil
		case <-ticker.C:
			if ownedPort(port, cmd.Process.Pid) != nil {
				continue
			}
			if healthy(m.ctx, port, manifest.Path) {
				if ownedPort(port, cmd.Process.Pid) != nil {
					continue
				}
				en.view.State = "running"
				en.view.Message = "预览正在运行；仅支持 GET/HEAD，应用登录、WebSocket/HMR 与跨源资源暂不支持"
				return m.snapshot(en), nil
			}
		}
	}
}
func (m *Manager) stopEntry(e *entry, message string) error {
	for key, token := range m.tokens {
		if token.entry == e {
			delete(m.tokens, key)
		}
	}
	if e.lease == nil {
		e.view.State = "stopped"
		e.view.Message = message
		return nil
	}
	select {
	case <-e.done:
	default:
		e.process.stop()
	}
	select {
	case <-e.done:
	case <-time.After(3 * time.Second):
		return errors.New("预览进程尚未可靠回收，工作区仍锁定")
	}
	if !groupGone(e.cmd.Process.Pid) {
		return errors.New("预览进程组尚未可靠回收，工作区仍锁定")
	}
	err := e.lease.Release(true)
	if err != nil {
		return err
	}
	e.lease = nil
	e.view.State = "stopped"
	e.view.Message = message
	return nil
}
func (m *Manager) Stop(req Request) (TaskPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, replay := m.operations[req.OperationID]
	if e := m.operation(req, "stop"); e != nil {
		return TaskPreview{}, e
	}
	if replay {
		return m.stateLocked(req)
	}
	st := m.store.Snapshot()
	if t, ok := st.Tasks[req.TaskID]; !ok || t.ProjectID != req.ProjectID {
		return TaskPreview{}, workbench.Missing()
	}
	if e := m.entries[req.TaskID]; e != nil {
		if e.req.RoundID != req.RoundID {
			return TaskPreview{}, workbench.Conflict("停止目标轮次已变化")
		}
		err := m.stopEntry(e, "预览已停止；成果和确认状态保留")
		return m.snapshot(e), err
	}
	return base(req, "stopped", "预览未运行"), nil
}

// StopTask is called outside Agent/Store locks, before a new writer starts.
func (m *Manager) StopTask(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[taskID]; e != nil {
		return m.stopEntry(e, "继续开发或合并前已停止预览")
	}
	return nil
}
func (m *Manager) Open(req Request, login string) (TaskPreview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, e := m.stateLocked(req)
	if e != nil {
		return v, e
	}
	if v.State != "running" || login == "" {
		return v, workbench.Conflict("预览未运行，请先明确启动")
	}
	for key, cap := range m.tokens {
		if time.Now().After(cap.expires) {
			delete(m.tokens, key)
		}
	}
	if len(m.tokens) >= 256 {
		return v, workbench.Conflict("预览链接过多，请稍后再试")
	}
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return v, e
	}
	token := hex.EncodeToString(raw)
	m.tokens[token] = &capability{m.entries[req.TaskID], login, time.Now().Add(30 * time.Minute)}
	v.URL = "/preview/" + token + v.Path
	return v, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	var err error
	for _, e := range m.entries {
		err = errors.Join(err, m.stopEntry(e, "服务退出，预览已停止"))
	}
	m.mu.Unlock()
	m.workers.Wait()
	return err
}

type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(b)
	const max = 64 << 10
	if len(b) >= max {
		t.b = append(t.b[:0], b[len(b)-max:]...)
	} else {
		overflow := len(t.b) + len(b) - max
		if overflow > 0 {
			t.b = append(t.b[:0], t.b[overflow:]...)
		}
		t.b = append(t.b, b...)
	}
	return n, nil
}
func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.ToValidUTF8(string(t.b), "?")
}
func previewEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOST=127.0.0.1", "BROWSER=none"}
	for _, key := range []string{"HOME", "TMPDIR", "SYSTEMROOT", "WINDIR"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func healthy(ctx context.Context, port int, path string) bool {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	res, e := upstreamClient().Do(r)
	if e != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 400
}
