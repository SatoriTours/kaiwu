package agent

import (
	"context"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"sync"
	"time"
)

type toolLease struct {
	lease *executionLease
	clear func() error
	once  sync.Once
}

func (l *toolLease) File() *os.File        { return l.lease.inheritedFile() }
func (l *toolLease) Attempt() error        { return l.lease.attempt() }
func (l *toolLease) Started(pid int) error { return l.lease.started(pid) }
func (l *toolLease) NoProcess()            { _ = l.lease.noProcess() }
func (l *toolLease) Release(safe bool) error {
	e := l.lease.release(safe)
	if e == nil {
		l.once.Do(func() { _ = l.clear() })
	}
	return e
}

// AcquireToolLease retains the reservation for the entire human write/process,
// rather than releasing it as soon as the idle pi has drained.
func (m *Manager) AcquireToolLease(ctx context.Context, projectID, taskID string) (preview.Lease, error) {
	st := m.store.Snapshot()
	p, ok := st.Projects[projectID]
	if !ok {
		return nil, workbench.Missing()
	}
	c := st.Conversations[p.ConversationID]
	if taskID != "" {
		t, ok := st.Tasks[taskID]
		if !ok || t.ProjectID != projectID || t.ExecutionID == "" {
			return nil, workbench.Missing()
		}
		c = st.Conversations[t.ConversationID]
	}
	if c.RunState == "starting" || c.RunState == "running" || workbench.TaskMerging(st, taskID) {
		return nil, workbench.Conflict("AI 或合并正在使用工作区，请等待结束")
	}
	if d := st.Deliveries[taskID]; workbench.DeliveryActive(d.Phase) && d.Phase != "paused" {
		return nil, workbench.Conflict("请先暂停自动交付")
	}
	var clear func() error
	var e error
	var lease *executionLease
	if taskID != "" {
		clear, e = m.reserveTask(ctx, taskID)
		if e != nil {
			return nil, e
		}
		st = m.store.Snapshot()
		t := st.Tasks[taskID]
		c = st.Conversations[t.ConversationID]
		x := st.Executions[t.ExecutionID]
		if c.RunState == "starting" || c.RunState == "running" || x.Workspace == "" {
			clear()
			return nil, workbench.Conflict("工作区状态已变化")
		}
		lease, e = acquireExecutionLease(x.Workspace)
	} else {
		clear, e = m.reserveProjectTools(ctx, p)
		if e != nil {
			return nil, e
		}
		common, err := gitops.CommonDir(ctx, p.Path)
		if err != nil {
			clear()
			return nil, err
		}
		lease, e = acquireProjectMergeLease(common)
	}
	if e != nil {
		clear()
		return nil, e
	}
	return &toolLease{lease: lease, clear: clear}, nil
}
func (m *Manager) reserveProjectTools(ctx context.Context, p workbench.Project) (func() error, error) {
	key := "project:" + p.ID
	m.mu.Lock()
	c := m.store.Snapshot().Conversations[p.ConversationID]
	if m.closed || m.reserved[key] || c.RunState == "starting" || c.RunState == "running" {
		m.mu.Unlock()
		return nil, workbench.Conflict("项目工作区正在使用")
	}
	entry := m.instances[c.ID]
	if entry != nil && entry.busy {
		m.mu.Unlock()
		return nil, workbench.Conflict("项目 AI 正在执行")
	}
	m.reserved[key] = true
	if entry != nil {
		entry.dead = true
	}
	m.mu.Unlock()
	clear := func() error { m.mu.Lock(); delete(m.reserved, key); m.mu.Unlock(); return nil }
	if entry != nil && entry.client != nil {
		cleanup, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		_ = entry.client.Close(cleanup)
		r, e := entry.client.Wait(cleanup)
		if e != nil || !r.Reaped || r.CleanupError != nil {
			clear()
			return nil, errors.New("原项目 pi 尚未可靠退出")
		}
		if entry.consumerDone != nil {
			select {
			case <-entry.consumerDone:
			case <-cleanup.Done():
				clear()
				return nil, cleanup.Err()
			}
		}
	}
	m.mu.Lock()
	delete(m.instances, c.ID)
	m.mu.Unlock()
	return clear, nil
}

// ToolWriteBlocked is an admission hint; begin and the OS lease remain the race-safe gate.
func (m *Manager) ToolWriteBlocked(projectID, taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reserved["project:"+projectID] || m.reserved[taskID]
}
