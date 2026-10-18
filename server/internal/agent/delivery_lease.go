package agent

import (
	"context"
	"errors"
	"os"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// DeliveryLease hands the task's single writer slot to a delivery stage: the
// inherited lock file plus a durable ownership marker (Task 5 ProcessLease)
// and a release switch that keeps the reservation parked until Release.
type DeliveryLease interface {
	gitops.ProcessLease
	Release(safe bool) error
}

type deliveryLease struct {
	clear         func()
	project, task *executionLease
}

func (l *deliveryLease) File() *os.File {
	if l.task != nil {
		return l.task.inheritedFile()
	}
	if l.project != nil {
		return l.project.inheritedFile()
	}
	return nil
}
func (l *deliveryLease) Attempt() error {
	var first error
	if l.project != nil {
		first = l.project.attempt()
	}
	return errors.Join(first, l.task.attempt())
}
func (l *deliveryLease) Started(pid int) error {
	var first error
	if l.project != nil {
		first = l.project.started(pid)
	}
	return errors.Join(first, l.task.started(pid))
}

// Release keeps unconfirmed launch windows failed closed (safe=false) and
// only clears the reservation when it succeeds.
func (l *deliveryLease) Release(safe bool) error {
	var e error
	if l.task != nil {
		e = errors.Join(e, l.task.release(safe))
	}
	if l.project != nil {
		e = errors.Join(e, l.project.release(safe))
	}
	if l.clear != nil {
		l.clear()
		l.clear = nil
	}
	return e
}

// AcquireDeliveryLease drains the idle original pi reliably (never while it
// executes), then takes the project merge lock first and the task execution
// lock second for project-scoped stages. No network waiting happens here.
func (m *Manager) AcquireDeliveryLease(ctx context.Context, deliveryID, scope string) (DeliveryLease, error) {
	if scope != "task" && scope != "project_task" {
		return nil, workbench.Invalid("交付租约范围无效")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("manager closed")
	}
	if m.deliveryReserved == nil {
		m.deliveryReserved = map[string]bool{}
	}
	if m.deliveryReserved[deliveryID] {
		m.mu.Unlock()
		return nil, workbench.Conflict("交付租约已持有，请等待释放")
	}
	d, e := m.store.Delivery(deliveryID)
	if e != nil {
		m.mu.Unlock()
		return nil, e
	}
	m.deliveryReserved[deliveryID] = true
	m.mu.Unlock()
	clearRes := func() {
		m.mu.Lock()
		delete(m.deliveryReserved, deliveryID)
		m.mu.Unlock()
	}
	l := &deliveryLease{clear: clearRes}
	// A real effect preview owns the same execution lock. Stop and reap it
	// outside Agent/Store locks before reserving the task for delivery.
	if e := m.stopTaskPreview(ctx, d.TaskID); e != nil {
		clearRes()
		return nil, e
	}
	release, e := m.reserveTask(ctx, d.TaskID)
	if e != nil {
		clearRes()
		return nil, e
	}
	l.clear = func() {
		_ = release()
		clearRes()
	}
	st := m.store.Snapshot()
	t, ok := st.Tasks[d.TaskID]
	x := st.Executions[t.ExecutionID]
	if !ok || x.Workspace == "" {
		l.Release(true)
		return nil, errors.New("任务执行缺少工作区，未交付")
	}
	if scope == "project_task" {
		p := st.Projects[d.ProjectID]
		common, e := gitops.CommonDir(ctx, p.Path)
		if e != nil {
			l.Release(true)
			return nil, e
		}
		l.project, e = acquireProjectMergeLease(common)
		if e != nil {
			l.Release(true)
			return nil, e
		}
	}
	l.task, e = acquireExecutionLease(x.Workspace)
	if e != nil {
		l.Release(true)
		return nil, e
	}
	return l, nil
}
