package agent

import (
	"context"
	"errors"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func executionFor(st workbench.State, id string) (workbench.Task, workbench.Execution, bool) {
	c := st.Conversations[id]
	t, ok := st.Tasks[c.TaskID]
	if !ok || t.ExecutionID == "" {
		return t, workbench.Execution{}, false
	}
	x, ok := st.Executions[t.ExecutionID]
	return t, x, ok
}

func setExecutionPhase(st *workbench.State, id, phase, message string) {
	t, x, ok := executionFor(*st, id)
	if !ok {
		return
	}
	x.Phase = phase
	x.Error = message
	switch phase {
	case "failed":
		t.Status = "failed"
		x.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	case "interrupted":
		t.Status = "interrupted"
	default:
		t.Status = "running"
	}
	st.Executions[x.ID] = x
	st.Tasks[t.ID] = t
}

func settleExecution(st *workbench.State, id, responseID string, resumed bool) {
	t, x, ok := executionFor(*st, id)
	if !ok {
		return
	}
	// No tools, report or interaction means this settled turn only answered a
	// question. It cannot replace a real development result with an acceptance card.
	c := st.Conversations[id]
	if !resumed && len(c.Rounds) > 1 {
		last := &c.Rounds[len(c.Rounds)-1]
		previousConversation := c
		previousConversation.Rounds = c.Rounds[:len(c.Rounds)-1]
		previous := workbench.LatestDevelopmentRound(&previousConversation)
		if previous != nil && (previous.Phase == "finished" || previous.Phase == "confirmed") &&
			len(last.Tools) == 0 && last.Report == nil && last.ReportError == "" &&
			last.InteractionError == "" && last.Origin == "" {
			last.Phase = "answered"
			x.Phase = "awaiting_review"
			t.Status = "needs_confirmation"
			if previous.Phase == "confirmed" {
				x.Phase = "confirmed"
				t.Status = "completed"
			}
			x.Error = ""
			x.FinishedAt = previous.FinishedAt
			x.Tools = append([]workbench.ToolRecord{}, previous.Tools...)
			x.Summary = ""
			active := false
			for _, msg := range c.Messages {
				if msg.ID == previous.ID {
					active = true
					continue
				}
				if active && msg.Role == "user" {
					break
				}
				if active && msg.Role == "assistant" {
					x.Summary = msg.Text
				}
			}
			if previous.Report != nil {
				x.Summary = previous.Report.Summary
			}
			st.Conversations[id] = c
			st.Executions[x.ID] = x
			st.Tasks[t.ID] = t
			return
		}
	}
	x.Phase = "awaiting_review"
	x.Error = ""
	t.Status = "needs_confirmation"
	if !resumed {
		for _, msg := range st.Conversations[id].Messages {
			if msg.ID == responseID {
				x.Summary = msg.Text
			}
		}
		if x.Summary == "" {
			x.Summary = "本轮执行已结束，未收到文字摘要；请检查任务结果。"
		}
		x.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	st.Executions[x.ID] = x
	st.Tasks[t.ID] = t
}

// Called only by the reserved conversation writer. Stop and drain the old
// consumer before reusing the original session file with broader tools.
func (m *Manager) prepareDevelopment(c workbench.Conversation, entry *instance, restoring bool) error {
	t, x, ok := executionFor(m.store.Snapshot(), c.ID)
	if !ok {
		return nil
	}
	if !pirpc.ProcessGroupsSupported() {
		return errors.New("当前平台尚不支持开发进程组管理")
	}
	if !restoring {
		if e := m.store.Update(func(st *workbench.State) error { setExecutionPhase(st, c.ID, "preparing", ""); return nil }); e != nil {
			return e
		}
	}
	prepared, e := m.prepareWorkspace(m.ctx, t, x)
	if e != nil {
		return e
	}
	if e = m.store.Update(func(st *workbench.State) error {
		if !restoring {
			prepared.Phase = "starting"
		}
		st.Executions[prepared.ID] = prepared
		return nil
	}); e != nil {
		return e
	}
	m.mu.Lock()
	if entry.lease == nil {
		lease, err := acquireExecutionLease(prepared.Workspace)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		entry.lease = lease
	}
	old := entry.client
	changed := old != nil && entry.workspace != prepared.Workspace
	done := entry.consumerDone
	if changed {
		entry.dead = true
	}
	m.mu.Unlock()
	if changed {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err := old.Close(ctx)
		result, waitErr := old.Wait(ctx)
		cancel()
		if err != nil || waitErr != nil || !result.Reaped || result.CleanupError != nil {
			return errors.New("原进程尚未可靠回收，未启动开发进程")
		}
		if done != nil {
			select {
			case <-done:
			case <-m.ctx.Done():
				return m.ctx.Err()
			}
		}
		m.mu.Lock()
		entry.client = nil
		entry.consumerDone = nil
		entry.dead = false
		entry.runID = ""
		m.mu.Unlock()
		// The recycled process cannot answer an old dialog.
		_ = m.store.Update(func(st *workbench.State) error {
			workbench.ExpireNativeInteractions(st, c.ID, "原 pi 进程已更换，原生请求失效；请让 AI 重新发起")
			return nil
		})
	}
	m.mu.Lock()
	entry.workspace = prepared.Workspace
	m.mu.Unlock()
	return nil
}

func restoreExecution(st *workbench.State, id string, previous workbench.Execution) {
	t, x, ok := executionFor(*st, id)
	if !ok {
		return
	}
	x.Error = ""
	switch {
	case previous.Phase == "confirmed":
		x.Phase = "confirmed"
		t.Status = "completed"
	case previous.Phase == "awaiting_review" && previous.FinishedAt != "":
		x.Phase = "awaiting_review"
		t.Status = "needs_confirmation"
	default:
		x.Phase = "interrupted"
		t.Status = "interrupted"
		if previous.Summary == "" && len(previous.Tools) == 0 {
			messages := st.Conversations[id].Messages
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					if messages[i].Delivery == "not_sent" && workbench.IsStartRequest(messages[i].Text) {
						x.Phase = "ready"
						t.Status = "idle"
					}
					break
				}
			}
		}
	}
	st.Executions[x.ID] = x
	st.Tasks[t.ID] = t
}
