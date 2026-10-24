package agent

import (
	"context"
	"os"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Callbacks are copied under mu and invoked with neither the Agent nor Store
// lock held. Preview holds its own lock while reserving the task in Agent.
func (m *Manager) SetPreviewStopper(stop func(context.Context, string) error) {
	m.mu.Lock()
	m.previewStop = stop
	m.mu.Unlock()
}
func (m *Manager) stopTaskPreview(ctx context.Context, taskID string) error {
	if taskID == "" {
		return nil
	}
	m.mu.Lock()
	stop := m.previewStop
	m.mu.Unlock()
	if stop != nil {
		return stop(ctx, taskID)
	}
	return nil
}

type previewLease struct{ lease *executionLease }

func (l *previewLease) File() *os.File          { return l.lease.inheritedFile() }
func (l *previewLease) Attempt() error          { return l.lease.attempt() }
func (l *previewLease) Started(pid int) error   { return l.lease.started(pid) }
func (l *previewLease) Release(safe bool) error { return l.lease.release(safe) }
func (l *previewLease) NoProcess()              { _ = l.lease.noProcess() }

// Reserve drains the original idle pi before transferring its execution lock.
// Session identity, session file, messages and finished results remain intact.
func (m *Manager) AcquirePreviewLease(ctx context.Context, taskID string) (preview.Lease, error) {
	release, e := m.reserveTask(ctx, taskID)
	if e != nil {
		return nil, e
	}
	defer release()
	st := m.store.Snapshot()
	t, ok := st.Tasks[taskID]
	c := st.Conversations[t.ConversationID]
	x := st.Executions[t.ExecutionID]
	if !ok || x.Workspace == "" || c.RunState == "starting" || c.RunState == "running" || workbench.TaskMerging(st, taskID) {
		return nil, workbench.Conflict("任务开发或合并中，不能接管预览")
	}
	l, e := acquireExecutionLease(x.Workspace)
	if e != nil {
		return nil, e
	}
	return &previewLease{l}, nil
}
