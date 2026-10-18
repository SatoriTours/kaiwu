package delivery

import (
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// The integration lane: one persistent FIFO slot per clone and per remote main
// branch. The slot is taken after the task CI gate passes and kept until the
// main CI gate passes or the cycle is explicitly cancelled, so a failed main
// integration quarantines the lane instead of letting the next task stack on
// top of a broken baseline. Waiting for CI or acceptance never holds an OS
// write lock; the actual Git changes acquire the remote gate, the common-dir
// lock and the task write lease in that order for a short window.

// mainRef is the exact remote/local ref of the authorized main branch.
func mainRef(d workbench.Delivery) string {
	return "refs/heads/" + d.Policy.Repository.MainBranch
}

// mainGate is the exact observation key of the main stage: only the authorized
// main workflows and the integrated main SHA count.
func (m *Manager) mainGate(d workbench.Delivery) githubactions.RunKey {
	return githubactions.RunKey{
		RepositoryID:  d.Policy.Repository.ID,
		DeliveryID:    d.ID,
		Stage:         "main",
		Owner:         d.Policy.Repository.Owner,
		Repo:          d.Policy.Repository.Name,
		Branch:        d.Policy.Repository.MainBranch,
		SHA:           d.LocalMainSHA,
		CredentialRef: d.Policy.APICredentialRef,
		Required:      d.Policy.MainWorkflows,
	}
}

// mainRemoteSHA reads the reliable remote main baseline. An absent ref is only
// acceptable when the project explicitly authorized creating the main branch.
func (m *Manager) mainRemoteSHA(d workbench.Delivery, x workbench.Execution) (string, error) {
	sha, err := m.cfg.Remote.Observe(m.ctx, x.Workspace, remoteTarget(d.Policy, d.TaskID), mainRef(d), d.Policy.GitCredentialRef, nil)
	if err != nil {
		return "", err
	}
	if sha == "" && !d.Policy.AllowCreateMain {
		return "", errMainMissing
	}
	return sha, nil
}

type mainMissingError struct{}

func (mainMissingError) Error() string { return "远程主分支不存在且未授权创建" }

var errMainMissing = mainMissingError{}

// stagePreparingIntegration takes the FIFO turn, re-reads the remote main and
// brings the candidate onto it. A changed candidate reruns every gate; an
// unchanged candidate moves to acceptance (when required) or straight to the
// local main fast-forward.
func (m *Manager) stagePreparingIntegration(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, t, x, ok := executionOf(st, d)
	if !ok || x.Workspace == "" {
		return m.block(d, "workspace_missing", "任务执行缺少可用工作区")
	}
	if _, err := m.store.EnqueueDelivery(d.ID); err != nil {
		return m.block(d, "queue_unavailable", "无法进入集成队列："+err.Error())
	}
	mine, err := m.store.AcquireDeliveryTurn(d.ID)
	if err != nil {
		return m.block(d, "queue_unavailable", "集成队列轮次不可判定："+err.Error())
	}
	if !mine {
		// Waiting for the lane: the logical slot is persisted, no OS lock held.
		return d, false
	}
	remoteMain, err := m.mainRemoteSHA(d, x)
	if err != nil {
		return m.block(d, "main_remote_unavailable", "远程主分支不可读："+err.Error())
	}
	cand, err := gitops.SyncCandidate(m.ctx, p, t, x, d.BaseMainSHA, gitRunnerFunc, m.cfg.DataDir)
	if err != nil {
		if strings.Contains(err.Error(), "冲突") {
			return m.block(d, "integration_conflict", "任务候选与主分支冲突，需人工处理："+err.Error())
		}
		return m.block(d, "sync_unavailable", "候选无法与主分支同步："+err.Error())
	}
	next := d
	next.RemoteMainSHA = remoteMain
	if cand.HEAD != d.CandidateSHA {
		// The synced candidate changed: all gates rerun from local checking
		// while the integration slot stays reserved for this cycle.
		next.Phase = "local_checking"
		next.CandidateSHA = cand.HEAD
		next.CandidateTree = cand.Tree
		next.CIDeadline = time.Time{}
		sync := workbench.DeliveryStep{
			ID:               stepID(),
			DeliveryID:       d.ID,
			Kind:             "sync_main",
			InputFingerprint: cand.Fingerprint,
			Phase:            "observed",
			BeforeSHA:        d.CandidateSHA,
			AfterSHA:         cand.HEAD,
			Summary:          "已纳入最新主分支，重新执行全部门禁",
			ExitKnown:        true,
		}
		out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &sync)
		if err != nil {
			return d, false
		}
		return out, true
	}
	if d.Policy.RequireAcceptance {
		next.Phase = "awaiting_acceptance"
	} else {
		next.Phase = "integrating"
	}
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	return out, true
}

// stageIntegrating fast-forwards the local main to the verified candidate. The
// main checkout and its owner are re-verified first; unknown local ahead
// commits block instead of being overwritten.
func (m *Manager) stageIntegrating(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, _, _, ok := executionOf(st, d)
	if !ok {
		return m.block(d, "workspace_missing", "项目或任务已不存在")
	}
	lease := m.leaseFor(d.ID)
	if lease == nil {
		acquired, err := m.cfg.Agent.AcquireDeliveryLease(m.ctx, d.ID, "project_task")
		if err != nil {
			return d, false
		}
		m.holdLease(d.ID, acquired)
		lease = acquired
	}
	if err := gitops.MainReady(m.ctx, p, d.Policy.Repository.MainBranch); err != nil {
		return m.block(d, "integration_blocked", err.Error())
	}
	expected := d.RemoteMainSHA
	if expected == "" {
		expected = d.LocalMainSHA
	}
	sha, err := gitops.IntegrateCandidate(m.ctx, p, d.CandidateSHA, expected, gitRunnerFunc)
	if err != nil {
		return m.block(d, "integration_failed", "主分支快进未确认："+err.Error())
	}
	next := d
	next.LocalMainSHA = sha
	next.Phase = "integration_checking"
	step := workbench.DeliveryStep{
		ID:               stepID(),
		DeliveryID:       d.ID,
		Kind:             "integrate_main",
		InputFingerprint: sha,
		Phase:            "observed",
		AfterSHA:         sha,
		Ref:              mainRef(d),
		Summary:          "本地主分支已快进到已验证候选",
		ExitKnown:        true,
	}
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &step)
	if err != nil {
		return d, false
	}
	return out, true
}

// stageIntegrationChecking runs the configured local checks against the actual
// integrated main SHA. A failure repairs the original task worktree with local
// evidence; the integration slot stays quarantined.
func (m *Manager) stageIntegrationChecking(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, _, _, ok := executionOf(st, d)
	if !ok {
		return m.block(d, "workspace_missing", "项目或任务已不存在")
	}
	lease := m.leaseFor(d.ID)
	x := workbench.Execution{Workspace: p.Path, Branch: d.Policy.Repository.MainBranch}
	ev, err := RunChecks(m.ctx, x, d.Policy.Checks, lease)
	if leaseRefused(err) {
		return m.block(d, "writer_busy", "写入租约拒绝运行集成检查")
	}
	if err != nil || !ev.Passed {
		if err != nil {
			ev.Summary = clipRunes(ev.Summary+"\n环境失败："+err.Error(), workbench.RepairSummaryLimit)
		}
		return m.repairStage(d, githubactions.FailureEvidence{Code: "failure", Fingerprint: ev.Fingerprint}, &ev, "integration_checks")
	}
	next := d
	next.Phase = "pushing_main"
	check := workbench.DeliveryStep{
		ID:               stepID(),
		DeliveryID:       d.ID,
		Kind:             "integration_checks",
		InputFingerprint: d.LocalMainSHA,
		Phase:            "observed",
		AfterSHA:         d.LocalMainSHA,
		Tree:             ev.Tree,
		Summary:          ev.Summary,
		ExitKnown:        true,
	}
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &check)
	if err != nil {
		return d, false
	}
	return out, true
}

// stagePushingMain pushes exactly the verified main SHA to the authorized main
// ref, then arms the main CI gate deadline. The durable intent step precedes
// the irreversible remote write.
func (m *Manager) stagePushingMain(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, _, _, ok := executionOf(st, d)
	if !ok {
		return m.block(d, "workspace_missing", "项目或任务已不存在")
	}
	local, err := gitops.Head(m.ctx, p.Path, mainRef(d))
	if err != nil {
		return m.block(d, "integration_failed", "本地主分支不可读："+err.Error())
	}
	if local != d.LocalMainSHA {
		return m.block(d, "integration_changed", "本地主分支在推送前发生变化，请重新核查")
	}
	ref := mainRef(d)
	// A durable intent from a crashed process is resolved before the write is
	// repeated; only a provably unlanded intent proceeds to push.
	if out, handled, cont := m.resolvePendingIntent(d, "push_main"); handled {
		return out, cont
	}
	intent := workbench.DeliveryStep{
		ID:               stepID(),
		DeliveryID:       d.ID,
		Kind:             "push_main",
		InputFingerprint: d.LocalMainSHA,
		Phase:            "running",
		BeforeSHA:        d.RemoteMainSHA,
		Ref:              ref,
		Summary:          "拟推送主分支：" + ref,
		StartedAt:        m.now(),
	}
	seeded, err := m.store.AdvanceDelivery(d.ID, d.Version, d, &intent)
	if err != nil {
		return d, false
	}
	d = seeded
	if crashAt("after-main-intent") {
		return d, false
	}
	sha, err := m.cfg.Remote.Push(m.ctx, p.Path, remoteTarget(d.Policy, d.TaskID), d.LocalMainSHA, ref, d.Policy.GitCredentialRef, m.leaseFor(d.ID))
	if err != nil {
		return m.block(d, "main_push_unverified", "主分支推送未确认成功："+err.Error())
	}
	if crashAt("after-main-push") {
		return d, false
	}
	next := d
	next.Phase = "main_ci"
	next.CIDeadline = m.now().Add(dms(d.Policy.CISeconds))
	intent.Phase = "observed"
	intent.AfterSHA = sha
	intent.ExitKnown = true
	intent.FinishedAt = m.now()
	intent.Summary = "已推送主分支：" + ref + " @ " + sha
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &intent)
	if err != nil {
		return d, false
	}
	if crashAt("after-main-receipt") {
		return out, false
	}
	m.dropLease(d.ID, true)
	return out, true
}

// stageMainCI is passive over the exact main gate. A proven pass releases the
// integration slot as verified; a failure repairs the original task worktree
// with the exact evidence while the slot stays quarantined.
func (m *Manager) stageMainCI(d workbench.Delivery) (workbench.Delivery, bool) {
	now := m.now()
	if !d.CIDeadline.IsZero() && now.After(d.CIDeadline) {
		return m.block(d, "ci_deadline", "主分支 CI 门禁超出预算")
	}
	obs, err := m.cfg.Actions.Observe(m.ctx, m.mainGate(d))
	if err != nil {
		return m.block(d, "ci_protocol_error", "主分支 CI 观察协议错误："+err.Error())
	}
	sig := "main:" + ciSignature(obs.Runs)
	changed := sig != m.lastCI[d.ID]
	save := func() (workbench.Delivery, bool) {
		if !changed {
			return d, true
		}
		saved, err := m.store.SaveDeliveryCI(d.ID, d.Version, obs.Runs)
		if err != nil {
			return d, false
		}
		m.rememberCI(d.ID, sig)
		return saved, true
	}
	switch obs.State {
	case githubactions.StatePassed:
		d2, ok := save()
		if !ok {
			return d, false
		}
		next := d2
		next.Phase = "succeeded"
		next.FinishedAt = m.now()
		ci := workbench.DeliveryStep{
			ID:               stepID(),
			DeliveryID:       d.ID,
			Kind:             "ci_main",
			InputFingerprint: d2.LocalMainSHA,
			Phase:            "observed",
			BeforeSHA:        d2.LocalMainSHA,
			Summary:          "主分支 CI 门禁通过",
			ExitKnown:        true,
		}
		out, err := m.store.AdvanceDelivery(d.ID, d2.Version, next, &ci)
		if err != nil {
			return d2, false
		}
		// The verified release ends the quarantine in the same pass.
		_ = m.store.ReleaseDeliveryTurn(d.ID, false)
		return out, false
	case githubactions.StateFailed:
		d2, ok := save()
		if !ok {
			return d, false
		}
		plain := obs.Code
		if i := strings.Index(plain, ":"); i > 0 {
			plain = plain[:i]
		}
		ev := githubactions.FailureEvidence{Code: plain}
		if plain == "failure" {
			fetched, ferr := m.cfg.Actions.Failure(m.ctx, m.mainGate(d2), obs)
			if ferr != nil {
				return m.block(d2, "evidence_unavailable", "主分支失败证据不可用："+ferr.Error())
			}
			ev = fetched
		}
		if repairable, code := ClassifyFailure(ev, nil); !repairable {
			// The lane stays quarantined: a broken main baseline must not be
			// extended by the next task.
			return m.block(d2, code, "主分支失败原因不可自动修复，请人工复核")
		}
		return m.repairStage(d2, ev, nil, "main_ci")
	case githubactions.StateBlocked:
		return m.block(d, "ci_"+obs.Code, "主分支 CI 门禁无法判定："+obs.Code)
	default:
		if changed && len(obs.Runs) > 0 {
			if _, err := m.store.SaveDeliveryCI(d.ID, d.Version, obs.Runs); err == nil {
				m.rememberCI(d.ID, sig)
			}
		}
		return d, false
	}
}
