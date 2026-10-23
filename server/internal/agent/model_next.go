package agent

import (
	"context"
	"errors"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// recycleForModel closes a healthy idle pi instance so the next round can
// launch again with a different profile on the SAME session file. The caller
// either owns the entry's single busy round (in-run boundary switch) or
// verified the entry is idle (public ApplyPendingModel); a mid-request call
// is refused and leaves the old instance untouched.
func (m *Manager) recycleForModel(c workbench.Conversation, entry *instance, allowBusy bool) error {
	m.mu.Lock()
	busy := entry.busy
	client := entry.client
	if busy && !allowBusy {
		m.mu.Unlock()
		return workbench.Conflict("当前请求运行中，模型将在下一轮生效")
	}
	m.mu.Unlock()
	if client == nil {
		return nil
	}
	m.mu.Lock()
	entry.dead = true
	lease := entry.lease
	done := entry.consumerDone
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	e := client.Close(ctx)
	result, waitErr := client.Wait(ctx)
	cancel()
	confirmed := waitErr == nil && result.Reaped && result.CleanupError == nil
	if e != nil || !confirmed {
		return errors.New("原 pi 实例尚未可靠回收，未切换模型")
	}
	if lease != nil {
		if err := lease.release(true); err != nil {
			return err
		}
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
	entry.lease = nil
	entry.workspace = ""
	entry.responseID = ""
	entry.lastResponseID = ""
	entry.responseSequence = 0
	entry.lastError = false
	entry.lastErrorDetail = ""
	entry.acknowledged = false
	entry.settled = false
	m.mu.Unlock()
	return nil
}

// ApplyPendingModel recycles the idle original instance before the next round
// with a requested model change. It never touches a running request — the
// pending profile still applies there at the next round boundary — and never
// rewrites the server default template.
func (m *Manager) ApplyPendingModel(ctx context.Context, conversationID string) error {
	c, ok := m.store.Snapshot().Conversations[conversationID]
	if !ok {
		return workbench.Missing()
	}
	if c.PendingProfile == nil {
		return nil
	}
	m.mu.Lock()
	entry := m.instances[conversationID]
	m.mu.Unlock()
	if entry == nil {
		return nil
	}
	return m.recycleForModel(c, entry, false)
}
