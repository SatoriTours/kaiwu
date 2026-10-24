package agent

import (
	"context"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Reconciliation only observes evidence. It never resumes a commit or merge.
func (m *Manager) RecoverMerges(ctx context.Context) error {
	st := m.store.Snapshot()
	for _, op := range st.Merges {
		// needs_check needs an observation decision; the active phases may have
		// crashed after a real local effect and must be adopted under the leases.
		if !workbench.MergeIncomplete(op.Phase) && op.Phase != "needs_check" {
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
		if op.Mode == workbench.MergeModePush {
			// Push recovery only observes the frozen target. It never restarts a
			// Git write, never re-pushes and never replaces the authorised main
			// SHA with the current branch head.
			remoteSHA, contained := m.recoverPush(ctx, op, p, t, x, projectLease, taskLease)
			safe := m.releasePair(projectLease, taskLease)
			if !contained {
				continue
			}
			if !safe {
				// The observation proved the remote holds the intended commit, so
				// that fact is kept, but a writer group that survived may not be
				// reported as success nor lose its ownership marker.
				_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
					v.Phase = "needs_check"
					v.RemoteAfterSHA = remoteSHA
					if v.CIStatus == "" {
						v.CIStatus = "not_checked"
					}
					v.Error = "远程已确认收到合并提交，但写入进程组未确认退出，请核查后再处理"
					v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					return nil
				})
				continue
			}
			_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
				v.Phase = "succeeded"
				v.RemoteAfterSHA = remoteSHA
				if v.CIStatus == "" {
					v.CIStatus = "not_checked"
				}
				v.Error = ""
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
			continue
		}
		s, e := gitops.Check(ctx, p, t, x, m.cfg.DataDir)
		safe := m.releasePair(projectLease, taskLease)
		if e != nil {
			continue
		}
		_ = m.store.SaveGitStatus(s)
		auditTask := op.TaskAfterSHA
		if auditTask == "" {
			// The crash happened before the task commit SHA was recorded, so the
			// reviewed task SHA is the only authorised candidate.
			auditTask = op.Freeze().TaskSHA
		}
		if s.State == "merged" && auditTask != "" && s.TaskSHA == auditTask {
			if !safe {
				_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
					v.Phase = "needs_check"
					v.MainAfterSHA = s.MainSHA
					v.Error = "本地合并结果已确认，但写入进程组未确认退出，请核查后再处理"
					v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					return nil
				})
				continue
			}
			_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
				v.Phase = "succeeded"
				v.MainAfterSHA = s.MainSHA
				if v.IntendedSHA == "" {
					v.IntendedSHA = s.MainSHA
				}
				v.Error = ""
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
			continue
		}
		// A crashed writer that cannot be reconciled from local evidence must
		// stop holding the task: hand it to a human as needs_check instead of
		// staying active forever. No fact is invented and no Git write happens.
		if op.Phase != "needs_check" {
			_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
				v.Phase = "needs_check"
				v.MainAfterSHA = s.MainSHA
				if v.TaskAfterSHA == "" && s.TaskSHA != "" {
					v.TaskAfterSHA = s.TaskSHA
				}
				v.Error = "合并结果无法从本地证据确认，请核查后再处理"
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
		}
	}
	return nil
}

// recoverPush decides a push whose outcome was never confirmed. Only the remote
// observation and already-present local objects may decide; an unchanged
// ls-remote value or an error string proves nothing, so an unprovable result
// stays needs_check with its audit history intact.
func (m *Manager) recoverPush(ctx context.Context, op workbench.MergeOperation, p workbench.Project, t workbench.Task, x workbench.Execution, projectLease, taskLease *executionLease) (remoteSHA string, contained bool) {
	freeze := op.Freeze()
	if freeze.RemoteURL == "" || freeze.MainRef == "" {
		return "", false
	}
	if op.IntendedSHA == "" {
		// The crash happened before any push intent was recorded, so nothing can
		// have been uploaded. Re-derive the local facts from Git only: recovery
		// never pushes a commit the review did not intend.
		s, e := gitops.Check(ctx, p, t, x, m.cfg.DataDir)
		if e != nil {
			return "", false
		}
		_ = m.store.SaveGitStatus(s)
		auditTask := op.TaskAfterSHA
		if auditTask == "" {
			auditTask = freeze.TaskSHA
		}
		if s.State == "merged" && auditTask != "" && s.TaskSHA == auditTask {
			_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
				v.Phase = "needs_check"
				v.MainAfterSHA = s.MainSHA
				if v.TaskAfterSHA == "" {
					// Only the SHA just verified against the reviewed task commit.
					v.TaskAfterSHA = s.TaskSHA
				}
				v.Error = "本地合并已完成，但推送前记录未能保存，请核查后重新审核推送"
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				return nil
			})
		}
		return "", false
	}
	m.mu.Lock()
	runner := m.cfg.Remote
	m.mu.Unlock()
	if runner == nil {
		return "", false
	}
	remote := gitops.ManualRemote{Name: freeze.RemoteName, URL: freeze.RemoteURL, MainRef: freeze.MainRef, CredentialRef: freeze.CredentialRef}
	target := gitops.RemoteTargetOf(remote)
	lease := mergeLeaseAdapter{project: projectLease, task: taskLease}
	// A configuration change invalidates the frozen authorisation; it does not
	// inherit approval and must not be used to observe a different remote.
	if current, e := gitops.ResolveManualRemote(ctx, p); e != nil || current.Name != remote.Name || current.URL != remote.URL || current.MainRef != remote.MainRef || current.CredentialRef != remote.CredentialRef {
		return "", false
	}
	observed, e := runner.Observe(ctx, p.Path, target, remote.MainRef, remote.CredentialRef, lease)
	if e != nil {
		return "", false
	}
	contained = observed == op.IntendedSHA
	if !contained && observed != "" {
		// The remote commit must already be a local object; no fetch is allowed,
		// and only a proven containment counts as success.
		if _, e := gitops.CommitParents(ctx, p.Path, observed); e == nil {
			anc, e := gitops.Ancestor(ctx, p.Path, op.IntendedSHA, observed)
			contained = e == nil && anc
		}
	}
	if !contained {
		_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
			v.Phase = "needs_check"
			if v.Error == "" {
				v.Error = "推送结果未确认，本地合并已保留，请观察远程后再处理"
			}
			return nil
		})
		return "", false
	}
	return observed, true
}

// releasePair closes both leases and reports whether ownership could be proven
// safe. A live or unknown writer group keeps the durable marker, so the caller
// may neither publish success nor admit a second owner.
func (m *Manager) releasePair(projectLease, taskLease *executionLease) bool {
	taskErr := taskLease.release(true)
	projectErr := projectLease.release(true)
	return taskErr == nil && projectErr == nil && !taskLease.unproven() && !projectLease.unproven()
}
