// Package agent owns independent pi instances for the server lifetime.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"sync"
	"time"
)

type instance struct {
	client                *pirpc.Client
	busy, dead            bool
	messageID, responseID string
	lastResponseID        string
	responseSequence      int
	lastError             bool
	acknowledged, settled bool
	workspace             string
	consumerDone          chan struct{}
	lease                 *executionLease
}
type Manager struct {
	previewStop      func(context.Context, string) error
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	store            *workbench.Store
	cfg              Config
	instances        map[string]*instance
	seen             map[string]bool
	previews         map[string]workbench.MergePreview
	reserved         map[string]bool
	deliveryReserved map[string]bool
	refreshes        map[string]bool
	gitSlots         chan struct{}
	projectGates     map[string]chan struct{}
	closed           bool
	wg               sync.WaitGroup
}

func New(ctx context.Context, store *workbench.Store, cfg Config) *Manager {
	owned, cancel := context.WithCancel(ctx)
	return &Manager{ctx: owned, cancel: cancel, store: store, cfg: cfg, instances: map[string]*instance{}, seen: map[string]bool{}, previews: map[string]workbench.MergePreview{}, reserved: map[string]bool{}, deliveryReserved: map[string]bool{}, refreshes: map[string]bool{}, gitSlots: make(chan struct{}, 2)}
}
func (m *Manager) Count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.instances) }
func (m *Manager) Send(conversationID, messageID string) error {
	return m.begin(conversationID, messageID)
}
func (m *Manager) Resume(conversationID string) error { return m.begin(conversationID, "") }
func (m *Manager) begin(conversationID, messageID string) error {
	// Store admission publishes starting before this call. A concurrent preview
	// sees that state and cannot start while this callback drains its process.
	if c := m.store.Snapshot().Conversations[conversationID]; c.RunState == "starting" {
		if e := m.stopTaskPreview(m.ctx, c.TaskID); e != nil {
			return e
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("manager closed")
	}
	if messageID != "" && m.seen[messageID] {
		return nil
	}
	c, ok := m.store.Snapshot().Conversations[conversationID]
	if !ok || c.RunState != "starting" || m.reserved[c.TaskID] || workbench.TaskMerging(m.store.Snapshot(), c.TaskID) {
		return workbench.Conflict("会话不能启动")
	}
	entry := m.instances[conversationID]
	if entry != nil && entry.dead {
		if entry.client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			result, e := entry.client.Wait(ctx)
			cancel()
			if e != nil || !result.Reaped || result.CleanupError != nil {
				return errors.New("previous process cleanup unconfirmed")
			}
		}
		if entry.lease != nil {
			_ = entry.lease.release(true)
			entry.lease = nil
		}
		delete(m.instances, conversationID)
		entry = nil
	}
	if entry == nil {
		if len(m.instances) >= 8 {
			return errors.New("session capacity reached")
		}
		entry = &instance{}
		m.instances[conversationID] = entry
	}
	if entry.busy {
		return workbench.Conflict("会话忙碌")
	}
	entry.busy = true
	entry.messageID = messageID
	entry.responseID = ""
	entry.lastResponseID = ""
	entry.responseSequence = 0
	entry.lastError = false
	entry.acknowledged = false
	entry.settled = false
	if messageID != "" {
		m.seen[messageID] = true
	}
	m.wg.Add(1)
	go m.run(c, entry, messageID)
	return nil
}
func (m *Manager) run(c workbench.Conversation, entry *instance, messageID string) {
	defer m.wg.Done()
	_, previousExecution, _ := executionFor(m.store.Snapshot(), c.ID)
	pending := c.PendingProfile
	// An explicit next-round model request applies here, at the round
	// boundary: recycle the settled instance first, never interrupt a
	// running request, never silently fall back to the old model.
	if pending != nil && messageID != "" {
		if e := m.recycleForModel(c, entry, true); e != nil {
			m.failed(c.ID, "模型切换失败，本轮请求未发送："+e.Error(), false)
			m.release(c.ID, entry, false)
			return
		}
	}
	if e := m.prepareDevelopment(c, entry, messageID == ""); e != nil {
		m.failed(c.ID, e.Error(), false)
		m.release(c.ID, entry, false)
		return
	}
	m.mu.Lock()
	client := entry.client
	m.mu.Unlock()
	if client == nil {
		if pending != nil && messageID != "" {
			local := *pending
			c.Profile = local
		}
		spec, e := m.launch(c)
		if e != nil {
			m.failed(c.ID, "无法准备原会话目录或工作区；未创建替代上下文", false)
			m.release(c.ID, entry, true)
			return
		}
		if entry.lease != nil {
			if e = entry.lease.attempt(); e != nil {
				m.failed(c.ID, "无法保存启动归属，未启动 pi", false)
				m.release(c.ID, entry, true)
				return
			}
		}
		client, e = pirpc.Start(m.ctx, spec, pirpc.Options{})
		m.mu.Lock()
		entry.client = client
		m.mu.Unlock()
		if e != nil {
			m.failed(c.ID, "pi 启动或协议握手失败，请检查配置后恢复", false)
			m.release(c.ID, entry, true)
			return
		}
		if entry.lease != nil {
			if e = entry.lease.started(client.PID()); e != nil {
				m.stopFailed(c.ID, entry, client, "无法保存执行进程归属，已停止启动", true)
				return
			}
		}
		response, e := client.Call(m.ctx, pirpc.Command{Type: "get_state"})
		var state struct {
			SessionID    string `json:"sessionId"`
			SessionFile  string `json:"sessionFile"`
			IsStreaming  bool   `json:"isStreaming"`
			IsCompacting bool   `json:"isCompacting"`
			Pending      int    `json:"pendingMessageCount"`
			Model        *struct {
				ID       string `json:"id"`
				Provider string `json:"provider"`
			} `json:"model"`
		}
		restoring := c.SessionFile != "" && (hasHistory(c) || fileExists(c.SessionFile))
		if e != nil || json.Unmarshal(response.Data, &state) != nil || state.SessionID != c.SessionID || state.IsStreaming || state.IsCompacting || state.Pending != 0 || m.validateLocation(c, state.SessionFile) != nil || ((restoring || fileExists(state.SessionFile)) && m.validateSession(c, state.SessionFile) != nil) || (restoring && state.SessionFile != c.SessionFile) {
			m.stopFailed(c.ID, entry, client, "原会话文件或握手不匹配；未创建替代上下文", true)
			return
		}
		// When the launched process reports an actual model/provider, it must
		// match the requested profile before this request is allowed to send.
		// A mismatch can never be silently downgraded to the old model.
		if pending != nil && messageID != "" {
			honest := state.Model != nil && state.Model.ID == c.Profile.Model && state.Model.Provider == c.Profile.Provider
			if !honest {
				m.stopFailed(c.ID, entry, client, "实际模型未报告或与请求不符；本轮请求未发送，模型切换请求保留", false)
				return
			}
		}
		if e = m.store.Update(func(st *workbench.State) error {
			v := st.Conversations[c.ID]
			v.SessionFile = state.SessionFile
			if pending != nil && messageID != "" {
				newProfile := *pending
				v.Profile = newProfile
				v.PendingProfile = nil
			}
			st.Conversations[c.ID] = v
			return nil
		}); e != nil {
			m.stopFailed(c.ID, entry, client, "无法保存会话身份", true)
			return
		}
		m.mu.Lock()
		entry.consumerDone = make(chan struct{})
		m.mu.Unlock()
		m.wg.Add(1)
		go m.consume(c.ID, entry, client)
	}
	if messageID == "" {
		m.mu.Lock()
		_ = m.store.Update(func(st *workbench.State) error {
			v := st.Conversations[c.ID]
			v.RunState = "idle"
			v.Error = ""
			st.Conversations[c.ID] = v
			restoreExecution(st, c.ID, previousExecution)
			return nil
		})
		entry.busy = false
		m.mu.Unlock()
		return
	}
	var text string
	for _, message := range c.Messages {
		if message.ID == messageID {
			text = message.Text
		}
	}
	if c.TaskID != "" && workbench.IsStartRequest(text) {
		task := m.store.Snapshot().Tasks[c.TaskID]
		text = "开始开发当前任务。请先核对当前独立工作区已有修改，再继续实施；完成后说明修改和实际验证结果。不要自动提交、合并或推送。\n任务：" + task.Title + "\n要求：" + task.Request
	}
	if e := m.store.Update(func(st *workbench.State) error {
		v := st.Conversations[c.ID]
		v.RunState = "running"
		st.Conversations[c.ID] = v
		setExecutionPhase(st, c.ID, "running", "")
		return nil
	}); e != nil {
		m.stopFailed(c.ID, entry, client, "无法保存发送状态", false)
		return
	}
	params, _ := json.Marshal(map[string]string{"message": text + replyTurnInstructions})
	response, e := client.Call(m.ctx, pirpc.Command{Type: "prompt", Params: params})
	if e != nil {
		var rpc *pirpc.Error
		uncertain := errors.As(e, &rpc) && rpc.Delivery == pirpc.Uncertain
		m.stopFailed(c.ID, entry, client, "消息未能确认完成；请检查后明确恢复，不会自动重发", uncertain)
		return
	}
	var result struct {
		Disposition string `json:"disposition"`
	}
	_ = json.Unmarshal(response.Data, &result)
	m.mu.Lock()
	entry.acknowledged = true
	if result.Disposition == "handled" {
		entry.settled = true
	}
	e = m.store.Update(func(st *workbench.State) error {
		v := st.Conversations[c.ID]
		for i := range v.Messages {
			if v.Messages[i].ID == messageID && v.Messages[i].Delivery == "pending" {
				v.Messages[i].Delivery = "accepted"
			}
		}
		st.Conversations[c.ID] = v
		return nil
	})
	if e == nil && entry.settled {
		e = m.finishLocked(c.ID, entry)
	}
	m.mu.Unlock()
	if e != nil {
		m.stopFailed(c.ID, entry, client, "无法保存轮次状态，请检查存储后恢复", true)
	}
}

// Caller holds mu. A settled event alone cannot release an unacknowledged prompt.
func (m *Manager) finishLocked(id string, entry *instance) error {
	if !entry.acknowledged || !entry.settled {
		return nil
	}
	if entry.lastError {
		m.failed(id, "模型调用失败或被中断；已有内容保留", false)
		entry.busy = false
		return nil
	}
	e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[id]
		if c.RunState == "running" {
			c.RunState = "idle"
			for i := range c.Messages {
				if c.Messages[i].ID == entry.messageID || c.Messages[i].ID == entry.responseID {
					c.Messages[i].Delivery = "complete"
				}
			}
		}
		st.Conversations[id] = c
		responseID := entry.responseID
		if responseID == "" {
			responseID = entry.lastResponseID
		}
		for i := range c.Messages {
			if c.Messages[i].ID == responseID {
				c.Messages[i].Kind = "summary"
				c.Messages[i].Delivery = "complete"
			}
		}
		st.Conversations[id] = c
		workbench.UpdateRound(st, id, entry.messageID, func(r *workbench.Round) { r.Phase = "finished"; r.FinishedAt = time.Now().UTC().Format(time.RFC3339) })
		settleExecution(st, id, responseID, false)
		return nil
	})
	if e == nil {
		entry.busy = false
	}
	return e
}
func (m *Manager) stopFailed(id string, entry *instance, client *pirpc.Client, message string, uncertain bool) {
	m.mu.Lock()
	if entry.dead {
		m.mu.Unlock()
		return
	}
	entry.dead = true
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	_ = client.Close(ctx)
	result, waitErr := client.Wait(ctx)
	cancel()
	m.mu.Lock()
	if entry.lease != nil {
		_ = entry.lease.release(waitErr == nil && result.Reaped && result.CleanupError == nil)
		entry.lease = nil
	}
	entry.busy = false
	m.failed(id, message, uncertain)
	m.mu.Unlock()
}
func (m *Manager) release(id string, entry *instance, dead bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.busy = false
	entry.dead = entry.dead || dead
	if entry.lease != nil && (entry.client == nil || entry.lease.unused()) {
		_ = entry.lease.release(false)
		entry.lease = nil
	}
	if entry.client == nil {
		delete(m.instances, id)
	}
}
func (m *Manager) failed(id, message string, uncertain bool) {
	_ = m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[id]
		_, result, hasExecution := executionFor(*st, id)
		preserveResult := (uncertain && c.RunState == "idle") || (hasExecution && (result.Phase == "awaiting_review" || result.Phase == "confirmed"))
		c.RunState = "failed"
		if uncertain {
			c.RunState = "interrupted"
		}
		c.Error = message
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" || c.Messages[i].Delivery == "accepted" {
				c.Messages[i].Delivery = "failed"
				if uncertain {
					c.Messages[i].Delivery = "uncertain"
				}
			}
		}
		st.Conversations[id] = c
		phase := "failed"
		if uncertain {
			phase = "interrupted"
		}
		for i := range c.Rounds {
			if c.Rounds[i].Phase == "running" {
				c.Rounds[i].Phase = phase
				c.Rounds[i].FinishedAt = time.Now().UTC().Format(time.RFC3339)
			}
		}
		st.Conversations[id] = c
		if !preserveResult {
			setExecutionPhase(st, id, phase, message)
		}
		return nil
	})
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	var all error
	for id, entry := range m.instances {
		confirmed := false
		if entry.client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			result, e := entry.client.Wait(ctx)
			cancel()
			all = errors.Join(all, e, result.CleanupError)
			if !result.Reaped {
				all = errors.Join(all, errors.New("pi process not reaped"))
				continue
			}
			confirmed = e == nil && result.CleanupError == nil
		}
		if entry.lease != nil {
			all = errors.Join(all, entry.lease.release(confirmed))
			entry.lease = nil
		}
		delete(m.instances, id)
	}
	return all
}
