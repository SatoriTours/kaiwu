package agent

import (
	"context"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"time"
)

func (m *Manager) RefreshGit(ctx context.Context, projectID string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("manager closed")
	}
	if m.refreshes[projectID] {
		m.mu.Unlock()
		return nil
	}
	select {
	case m.gitSlots <- struct{}{}:
	default:
		m.mu.Unlock()
		return workbench.Conflict("Git 检查繁忙，请稍后重试")
	}
	m.wg.Add(1)
	m.refreshes[projectID] = true
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel(); m.wg.Done() }()
	defer func() { m.mu.Lock(); delete(m.refreshes, projectID); m.mu.Unlock(); <-m.gitSlots }()
	p, e := m.ensureMain(ctx, projectID)
	if e != nil {
		return e
	}
	st := m.store.Snapshot()
	for _, t := range st.Tasks {
		if t.ProjectID != projectID {
			continue
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		status, e := gitops.Check(ctx, p, t, st.Executions[t.ExecutionID], m.cfg.DataDir)
		if e != nil {
			return e
		}
		if e = m.store.SaveGitStatus(status); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) UpdateProjectGit(ctx context.Context, c workbench.Command) (workbench.Receipt, error) {
	before := m.store.Snapshot().Revision
	p, ok := m.store.Snapshot().Projects[c.ProjectID]
	if !ok {
		return workbench.Receipt{}, workbench.Missing()
	}
	// A manual target must remain usable when automatic detection is ambiguous.
	// Preserve/freeze the automatic default whenever it can be identified.
	if c.MainBranchOverride == "" || p.DefaultBranch != "" {
		var e error
		p, e = m.ensureMain(ctx, c.ProjectID)
		if e != nil {
			return workbench.Receipt{}, e
		}
	} else if _, e := gitops.DetectMain(ctx, p.Path); e == nil {
		p, e = m.ensureMain(ctx, c.ProjectID)
		if e != nil {
			return workbench.Receipt{}, e
		}
	}
	p.MainBranchOverride = c.MainBranchOverride
	if _, e := gitops.ResolveMain(ctx, p); e != nil {
		return workbench.Receipt{}, workbench.Invalid(e.Error())
	}
	if c.Revision == before {
		c.Revision = m.store.Snapshot().Revision
	}
	r, e := m.store.Command(c)
	return r, e
}
