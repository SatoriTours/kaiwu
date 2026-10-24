package agent

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// PreviewPushReview authorises one manual push. The accepted review is cached
// under its own preview ID so StartMerge can re-read exactly the reviewed
// remote facts; the queueing request carries only that identifier.
func (m *Manager) PreviewPushReview(ctx context.Context, review workbench.MergeReviewRequest) (workbench.MergeReview, error) {
	if review.Mode != workbench.MergeModePush {
		return workbench.MergeReview{}, workbench.Conflict("请以推送模式发起远程审核")
	}
	local, e := m.previewMerge(ctx, review.ProjectID, review.TaskID)
	if e != nil {
		return workbench.MergeReview{}, e
	}
	// Admission below re-checks the acknowledged round, so a review that can
	// never be queued is refused here instead of being advertised.
	if _, e := workbench.ReviewedTask(m.store.Snapshot(), workbench.MergeRequest{ProjectID: review.ProjectID, TaskID: review.TaskID, Mode: review.Mode, RoundID: review.RoundID}); e != nil {
		return workbench.MergeReview{}, e
	}
	base := local
	base.Mode, base.RoundID = "", ""
	st := m.store.Snapshot()
	p, ok := st.Projects[review.ProjectID]
	if !ok {
		return workbench.MergeReview{}, workbench.Missing()
	}
	t, ok := st.Tasks[review.TaskID]
	if !ok || t.ProjectID != review.ProjectID {
		return workbench.MergeReview{}, workbench.Missing()
	}
	x, ok := st.Executions[t.ExecutionID]
	if !ok {
		return workbench.MergeReview{}, workbench.Missing()
	}
	m.mu.Lock()
	runner := m.cfg.Remote
	m.mu.Unlock()
	var out workbench.MergeReview
	if runner == nil {
		// Without a remote runner the local proof is still valid; only push is
		// blocked, and it says so instead of failing the whole review.
		out = workbench.MergeReview{MergePreview: base, Uploads: []workbench.MergeUpload{}, Pending: []workbench.GitFile{}}
		out.PushReason = "服务未配置 Git 远程能力，无法审核推送"
	} else {
		out, e = gitops.ReviewRemote(ctx, runner, p, t, x, base, workbench.MergeReviewRequest{ProjectID: review.ProjectID, TaskID: review.TaskID, Mode: review.Mode, RoundID: review.RoundID}, m.cfg.DataDir)
		if e != nil {
			return workbench.MergeReview{}, e
		}
	}
	out.Mode, out.RoundID = review.Mode, review.RoundID
	out.ID = local.ID
	out.ExpiresAt = local.ExpiresAt
	// Cache the whole remote review under the same preview ID the client will
	// quote, replacing the local-only proof.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return out, errors.New("manager closed")
	}
	if _, ok := m.previews[local.ID]; !ok {
		return workbench.MergeReview{}, workbench.Conflict("预检已过期，请重新审核")
	}
	m.reviews[local.ID] = out
	return out, nil
}

// StartMerge admits a reviewed operation. Push admission additionally requires
// the complete frozen remote review, so no client-supplied URL, ref or SHA can
// enter an operation.
func (m *Manager) StartMerge(req workbench.MergeRequest) (workbench.MergeOperation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return workbench.MergeOperation{}, false, errors.New("manager closed")
	}
	if op, replay, e := m.store.FindMerge(req); replay || e != nil {
		return op, replay, e
	}
	proof, ok := m.previews[req.PreviewID]
	expire, _ := time.Parse(time.RFC3339, proof.ExpiresAt)
	if !ok || time.Now().After(expire) {
		return workbench.MergeOperation{}, false, workbench.Conflict("预检已过期，请重新审核")
	}
	if req.Mode == workbench.MergeModePush {
		review, ok := m.reviews[req.PreviewID]
		if !ok {
			return workbench.MergeOperation{}, false, workbench.Conflict("推送审核已过期，请重新审核")
		}
		if review.Mode != workbench.MergeModePush || review.RoundID != req.RoundID || review.ID != req.PreviewID {
			return workbench.MergeOperation{}, false, workbench.Conflict("审核凭据与请求不一致，请重新审核")
		}
		op, replay, e := m.store.BeginMergeReview(req, review)
		if e != nil || replay {
			return op, replay, e
		}
		m.wg.Add(1)
		go func() { defer m.wg.Done(); m.runPushMerge(op, op.Freeze(), op.RoundID) }()
		return op, false, nil
	}
	op, replay, e := m.store.BeginMerge(req, proof)
	if e != nil || replay {
		return op, replay, e
	}
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.runMerge(op) }()
	return op, false, nil
}

// mergeLeaseAdapter exposes the project→task lease pair to the remote runner so
// every remote git process records the same durable ownership marker.
type mergeLeaseAdapter struct {
	project, task *executionLease
	onFinish      func(pid int, gone bool)
}

func (a mergeLeaseAdapter) File() *os.File {
	if a.task != nil {
		return a.task.inheritedFile()
	}
	return a.project.inheritedFile()
}
func (a mergeLeaseAdapter) Attempt() error {
	return errors.Join(a.project.attempt(), a.task.attempt())
}
func (a mergeLeaseAdapter) Started(pid int) error {
	return errors.Join(a.project.started(pid), a.task.started(pid))
}

// NoProcess clears this launch window when the runner proves no child started.
func (a mergeLeaseAdapter) NoProcess() {
	_ = a.project.noProcess()
	_ = a.task.noProcess()
}

// Finished is called by the remote runner with the real fate of the writer's
// process group, so a push whose group survived can never be treated as safe.
func (a mergeLeaseAdapter) Finished(pid int, gone bool) {
	if a.onFinish != nil {
		a.onFinish(pid, gone)
	}
}

// runPushMerge executes the reviewed push inside the same project→task lease as
// the local merge, keeps every local fact it produced, and only reports remote
// success after observing the frozen target.
func (m *Manager) runPushMerge(op workbench.MergeOperation, freeze workbench.PushReviewFreeze, roundID string) {
	op.RoundID = roundID
	fail := ""
	// safe records whether every Git process group this operation started was
	// confirmed gone. It is read by finishPush after the deferred lease releases,
	// so an unconfirmed group never publishes success nor frees ownership.
	safe := true
	// checkNote marks outcomes that are neither a clean success nor a definite
	// failure: a real local effect happened but its record could not be saved.
	checkNote := ""
	// The leases decide safety themselves: an unproven writer group keeps the
	// marker and may never be reported as a clean success, whatever a transport
	// callback claims.
	var projectLease, taskLease *executionLease
	defer func() {
		if projectLease != nil && projectLease.unproven() {
			safe = false
		}
		if taskLease != nil && taskLease.unproven() {
			safe = false
		}
		m.finishPush(op.ID, fail, checkNote, safe)
	}()
	ctx := m.ctx
	p, e := m.ensureMain(ctx, op.ProjectID)
	if e != nil {
		fail = e.Error()
		return
	}
	common, e := gitops.CommonDir(ctx, p.Path)
	if e != nil {
		fail = e.Error()
		return
	}
	gate := m.projectGate(common)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		fail = ctx.Err().Error()
		return
	}
	projectLease, e = acquireProjectMergeLease(common)
	if e != nil {
		fail = e.Error()
		return
	}
	defer func() { _ = projectLease.release(safe) }()
	if e = m.advancePush(op.ID, "preparing", nil); e != nil {
		fail = e.Error()
		return
	}
	if e = m.stopTaskPreview(ctx, op.TaskID); e != nil {
		fail = e.Error()
		return
	}
	release, e := m.reserveTask(ctx, op.TaskID)
	if e != nil {
		fail = e.Error()
		return
	}
	defer release()
	st := m.store.Snapshot()
	t, ok := st.Tasks[op.TaskID]
	if !ok {
		fail = "任务已不存在"
		return
	}
	x, ok := st.Executions[t.ExecutionID]
	if !ok {
		fail = "任务执行记录已不存在"
		return
	}
	taskLease, e = acquireExecutionLease(x.Workspace)
	if e != nil {
		fail = e.Error()
		return
	}
	defer func() { _ = taskLease.release(safe) }()
	runner := func(ctx context.Context, dir string, args ...string) (string, error) {
		if e := errors.Join(projectLease.attempt(), taskLease.attempt()); e != nil {
			return "", errors.New("无法保存 Git 启动归属，未执行命令")
		}
		pid := 0
		out, e := gitops.Run(ctx, dir, func(id int) error { pid = id; return errors.Join(projectLease.started(id), taskLease.started(id)) }, args...)
		if pid == 0 || !processGroupGone(pid) {
			safe = false
		}
		return out, e
	}
	// Re-verify every reviewed input inside the lease before any local effect.
	fresh, e := gitops.Preview(ctx, p, t, x, m.cfg.DataDir)
	if e != nil {
		fail = e.Error()
		return
	}
	// The reviewed basis must still hold. An already merged task has no local
	// preview fingerprint (the preview refuses it), so it is verified by the
	// reviewed local main/task SHAs plus the absence of new pending work.
	if fresh.MainSHA != freeze.LocalMainSHA || fresh.TaskSHA != freeze.TaskSHA {
		fail = "工作区、主分支或设置已变化，请重新审核后再推送"
		return
	}
	if !freeze.ContentProof {
		if fresh.Fingerprint != freeze.Fingerprint {
			fail = "工作区、主分支或设置已变化，请重新审核后再推送"
			return
		}
	} else {
		// The reviewed already-merged head must still be the current main; the
		// push-only operation may not inherit approval for a moved branch.
		if head, he := gitops.Head(ctx, p.Path, "refs/heads/"+fresh.MainBranch); he != nil || head != freeze.LocalMainSHA {
			fail = "工作区、主分支或设置已变化，请重新审核后再推送"
			return
		}
		if fresh.NeedsCommit {
			fail = "工作区出现新的待提交改动，请重新审核后再推送"
			return
		}
	}
	pending := workbench.PendingFiles(fresh)
	if e := verifyPending(ctx, x.Workspace, pending, freeze.PendingPaths()); e != nil {
		fail = e.Error()
		return
	}
	if fresh.MainBranch != p.DefaultBranch && fresh.MainBranch != p.MainBranchOverride {
		fail = "主分支已变化，请重新审核后再推送"
		return
	}
	remote := gitops.ManualRemote{Name: freeze.RemoteName, URL: freeze.RemoteURL, MainRef: freeze.MainRef, CredentialRef: freeze.CredentialRef}
	// Re-resolve the configured remote and refuse if it no longer matches the
	// reviewed frozen target: a changed configuration never inherits approval.
	current, e := gitops.ResolveManualRemote(ctx, p)
	if e != nil {
		fail = e.Error()
		return
	}
	if current.Name != remote.Name || current.URL != remote.URL || current.MainRef != remote.MainRef || current.CredentialRef != remote.CredentialRef {
		fail = "远程配置已变化，请重新审核后再推送"
		return
	}
	m.mu.Lock()
	remoteRunner := m.cfg.Remote
	m.mu.Unlock()
	if remoteRunner == nil {
		fail = "服务未配置 Git 远程能力，无法推送"
		return
	}
	target := gitops.RemoteTargetFromManual(remote)
	lease := mergeLeaseAdapter{project: projectLease, task: taskLease, onFinish: func(_ int, gone bool) {
		if !gone {
			safe = false
		}
	}}
	// The observed remote must still be the reviewed base; anything newer means
	// somebody else moved the branch and the approved upload set is stale.
	observed, e := remoteRunner.Observe(ctx, p.Path, target, remote.MainRef, remote.CredentialRef, lease)
	if e != nil {
		fail = "远程状态观察失败，未推送：" + e.Error()
		return
	}
	if observed != freeze.ObservedSHA {
		fail = "远程主分支已变化，请重新审核后再推送"
		return
	}
	sha := fresh.TaskSHA
	if fresh.NeedsCommit {
		if e = m.advancePush(op.ID, "committing", nil); e != nil {
			fail = e.Error()
			return
		}
		sha, e = gitops.CommitTask(ctx, x.Workspace, op.CommitMessage, fresh.CommitName, fresh.CommitEmail, runner, fresh)
		if e != nil {
			fail = e.Error()
			return
		}
		if e = m.commitFact(op.ID, sha); e != nil {
			// The commit really happened even though its record failed, so this is
			// not a definite failure: keep the concrete SHA when any later write
			// still lands and otherwise leave the active phase for reconciliation.
			if we := m.advancePush(op.ID, "needs_check", func(v *workbench.MergeOperation) {
				if v.TaskAfterSHA == "" {
					v.TaskAfterSHA = sha
				}
				v.Error = "任务提交已完成，但提交结果未能保存，请核查后再处理"
				v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			}); we != nil {
				checkNote = "任务提交已完成，但提交记录未能保存，请核查后再处理"
			}
			return
		}
	}
	var mainSHA string
	// A push-only review already has the reviewed content on main. Re-running a
	// commit or merge would rewrite ORIG_HEAD and the task branch for no reason,
	// so the local write is skipped and main is uploaded exactly as reviewed.
	if freeze.PushOnly {
		mainSHA = freeze.LocalMainSHA
		if e = m.advancePush(op.ID, "merged", func(v *workbench.MergeOperation) { v.TaskAfterSHA = sha; v.MainAfterSHA = mainSHA }); e != nil {
			fail = "推送事实未能保存，未推送"
			return
		}
	} else {
		if e = m.advancePush(op.ID, "merging", func(v *workbench.MergeOperation) { v.TaskAfterSHA = sha }); e != nil {
			fail = e.Error()
			return
		}
		mainSHA, e = gitops.MergeTask(ctx, p, fresh, sha, runner)
		if e != nil {
			fail = e.Error()
			_ = m.RefreshGit(ctx, p.ID)
			return
		}
		// The local merge fact is saved immediately and never cleared by a later
		// remote error: the branch really did move. If the write fails the merge
		// itself already happened, so the operation must stay open for a human
		// (needs_check) instead of being reported as a failure that pretends main
		// did not move.
		if e = m.saveMergedFact(op.ID, mainSHA); e != nil {
			checkNote = "本地合并已完成，但结果未能保存，请核查后再处理"
			return
		}
	}
	if mainSHA != freeze.LocalMainSHA && !mainSHAAllowed(ctx, p.Path, freeze, sha, mainSHA) {
		fail = "本地合并产生了未经审核的提交范围，已停止推送"
		return
	}
	// Persist the exact intended commit before the Git write so a lost receipt,
	// timeout or crash is never ambiguous.
	if e = m.advancePush(op.ID, "pushing", func(v *workbench.MergeOperation) {
		v.IntendedSHA = mainSHA
		v.PushAttemptedAt = time.Now().UTC().Format(time.RFC3339)
	}); e != nil {
		fail = "推送前的尝试记录未能保存，未推送"
		return
	}
	if _, e = remoteRunner.Push(ctx, p.Path, target, mainSHA, remote.MainRef, remote.CredentialRef, lease); e != nil {
		// Unknown outcome: only observation may decide, never a retry.
		if v, vErr := remoteRunner.Observe(ctx, p.Path, target, remote.MainRef, remote.CredentialRef, lease); vErr == nil && v == mainSHA {
			// Record the observed fact, but leave the terminal phase to
			// finishPush, which runs after the leases were released.
			_ = m.advancePush(op.ID, "remote_done", func(o *workbench.MergeOperation) {
				o.RemoteAfterSHA = mainSHA
				o.CIStatus = "not_checked"
			})
			return
		}
		fail = "推送结果未确认，本地合并已保留，请观察远程后再处理"
		_ = m.advancePush(op.ID, "needs_check", func(o *workbench.MergeOperation) { o.Error = fail })
		fail = ""
		return
	}
	_ = m.advancePush(op.ID, "remote_done", func(o *workbench.MergeOperation) {
		o.RemoteAfterSHA = mainSHA
		o.CIStatus = "not_checked"
	})
	status, se := gitops.Check(ctx, p, t, x, m.cfg.DataDir)
	if se == nil {
		_ = m.store.SaveGitStatus(status)
	}
}

// mainSHAAllowed proves the merged main commit carries exactly reviewed content:
// it is either the reviewed local main, the reviewed task commit, or the ordinary
// merge of exactly those two commits, proven by rebuilding that merge tree. A
// commit that merely claims the reviewed parents is refused.
func mainSHAAllowed(ctx context.Context, dir string, freeze workbench.PushReviewFreeze, taskSHA, mainSHA string) bool {
	if mainSHA == taskSHA || mainSHA == freeze.LocalMainSHA {
		return true
	}
	// Both reviewed commits must still be present locally; without them no
	// content proof is possible.
	if _, e := gitops.TreeOf(ctx, dir, freeze.LocalMainSHA); e != nil {
		return false
	}
	if _, e := gitops.TreeOf(ctx, dir, taskSHA); e != nil {
		return false
	}
	parents, e := gitops.CommitParents(ctx, dir, mainSHA)
	if e != nil || len(parents) < 1 || len(parents) > 2 {
		return false
	}
	for _, v := range parents {
		if v != freeze.LocalMainSHA && v != taskSHA {
			return false
		}
	}
	// Fast-forward shapes must be the reviewed commit itself, already checked.
	if len(parents) == 1 {
		return false
	}
	// A two-parent commit is only allowed when its tree is byte-identical to the
	// merge of exactly the reviewed pair, so no unreviewed path can ride along.
	expected, e := gitops.ExpectedMergeTree(ctx, dir, freeze.LocalMainSHA, taskSHA)
	if e != nil || len(expected) != 40 || !isHexValue(expected) {
		return false
	}
	actual, e := gitops.TreeOf(ctx, dir, mainSHA)
	if e != nil {
		return false
	}
	return actual == expected
}

func isHexValue(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return s != ""
}

func (m *Manager) advancePush(id, phase string, change func(*workbench.MergeOperation)) error {
	return m.store.AdvanceMerge(id, func(v *workbench.MergeOperation) error {
		if phase != "" {
			v.Phase = phase
		}
		if change != nil {
			change(v)
		}
		return nil
	})
}

// saveMergedFact persists the real local merge result, retrying a bounded number
// of times so a transient store failure does not discard a fact that is already
// true on disk.
func (m *Manager) saveMergedFact(id, mainSHA string) error {
	var e error
	for i := 0; i < 3; i++ {
		e = m.advancePush(id, "merged", func(v *workbench.MergeOperation) { v.MainAfterSHA = mainSHA })
		if e == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 20 * time.Millisecond)
	}
	return e
}

// commitFact records a task commit that already happened on disk, retrying a
// bounded number of times like the merge fact so a transient store failure does
// not turn a real commit into an unrecorded one.
func (m *Manager) commitFact(id, sha string) error {
	var e error
	for i := 0; i < 3; i++ {
		e = m.advancePush(id, "committed", func(v *workbench.MergeOperation) { v.TaskAfterSHA = sha })
		if e == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 20 * time.Millisecond)
	}
	return e
}

// finishPush publishes a terminal phase only after the leases are released.
func (m *Manager) finishPush(id, fail, checkNote string, safe bool) {
	phase, errText := "succeeded", ""
	if fail != "" {
		phase, errText = "failed", fail
	}
	if checkNote != "" {
		phase, errText = "needs_check", checkNote
	}
	if m.ctx.Err() != nil && fail != "" {
		phase = "needs_check"
	}
	if !safe {
		// The Git writer group could not be proven gone, so no terminal claim may
		// be published at all: a definite failed would hide the unproven owner and
		// a succeeded would lie. Keep the original cause verbatim.
		phase = "needs_check"
		switch {
		case fail != "":
			errText = fail + "；推送后的 Git 进程组未确认退出，请核查后再处理"
		case checkNote != "":
			errText = checkNote
		default:
			errText = "推送后的 Git 进程组未确认退出，请核查后再处理"
		}
	}
	_ = m.store.AdvanceMerge(id, func(v *workbench.MergeOperation) error {
		if phase == "succeeded" && v.Phase == "needs_check" && v.Error != "" {
			// An earlier step already handed this operation to a human, so a
			// later success claim may not erase that cause or its facts.
			v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			return nil
		}
		if phase == "succeeded" && v.RemoteAfterSHA == "" {
			// Never claim success without the observed remote containing the
			// intended commit.
			v.Phase = "needs_check"
			v.Error = "推送结果未确认，请观察远程后再处理"
			v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			return nil
		}
		v.Phase = phase
		if errText != "" {
			v.Error = errText
		}
		v.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// verifyPending bounds any commit this operation may create to the exact files
// the review disclosed: a new or different path is unreviewed work and stops the
// operation before any local effect.
func verifyPending(ctx context.Context, workspace string, pending []workbench.GitFile, allowed map[string]bool) error {
	if len(pending) == 0 {
		return nil
	}
	actual, e := gitops.Files(ctx, workspace)
	if e != nil {
		return e
	}
	for _, f := range actual {
		if !allowed[f.Path] {
			return errors.New("工作区出现未经审核的改动：" + f.Path + "，请重新审核后再推送")
		}
	}
	return nil
}
