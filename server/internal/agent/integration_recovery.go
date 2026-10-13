package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"time"
)

// Reconciliation only observes evidence. It never resumes a commit or merge.
func (m *Manager) RecoverMerges(ctx context.Context) error {
	st := m.store.Snapshot()
	for _, op := range st.Merges {
		if op.Phase != "needs_check" {
			continue
		}
		t, ok := st.Tasks[op.TaskID]
		if !ok {
			continue
		}
		p, ok := st.Projects[op.ProjectID]
		if !ok {
			continue
		}
		x := st.Executions[t.ExecutionID]
		if x.Workspace == "" {
			continue
		}
		// A reachable commit alone does not prove a crashed writer has finished.
		// Reuse the normal project→task lease order, without restarting any Git.
		common, e := gitops.CommonDir(ctx, p.Path)
		if e != nil {
			continue
		}
		projectLease, e := acquireProjectMergeLease(common)
		if e != nil {
			continue
		}
		taskLease, e := acquireExecutionLease(x.Workspace)
		if e != nil {
			_ = projectLease.release(true)
			continue
		}
		s, e := gitops.Check(ctx, p, t, x, m.cfg.DataDir)
		_ = taskLease.release(true)
		_ = projectLease.release(true)
		if e != nil {
			continue
		}
		_ = m.store.SaveGitStatus(s)
		if s.State == "merged" && op.TaskAfterSHA != "" && s.TaskSHA == op.TaskAfterSHA {
			_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
				v.Phase = "succeeded"
				v.MainAfterSHA = s.MainSHA
				v.Error = ""
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
		}
	}
	return nil
}
