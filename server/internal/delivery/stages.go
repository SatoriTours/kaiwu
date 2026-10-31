package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// gitRunnerFunc runs candidate-commit Git commands without extra process
// tracking: the delivery worker already holds the task write lease.
var gitRunnerFunc gitops.Runner = func(ctx context.Context, dir string, args ...string) (string, error) {
	return gitops.Run(ctx, dir, nil, args...)
}

func dms(seconds int64) time.Duration { return time.Second * time.Duration(seconds) }

// stageQueued synchronizes the durable safety baseline before the machine
// takes over: common dir, protected-scope fingerprint and local main state.
// The dispatch intent is persisted while the cycle is still queued, then the
// original conversation is sent exactly once — before the cycle becomes
// visible as developing — so observers never see a developed cycle without
// a recorded dispatch. A round that settled before the cycle opened still
// gets its single dispatch; restarts rely on the agent port deduping by
// message ID and on the persisted stage record.
func (m *Manager) stageQueued(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, t, x, ok := executionOf(st, d)
	// A new task's workspace is created by the original Agent.Send. Requiring
	// it before dispatch makes first-time delivery impossible. The candidate
	// and lease gates still require the prepared, owned workspace afterwards.
	if !ok {
		return m.block(d, "workspace_missing", "任务执行缺少可用工作区")
	}
	if _, found := m.roundSettled(st, d); !found {
		return m.block(d, "round_missing", "找不到周期对应的任务轮次")
	}
	common, err := gitops.CommonDir(m.ctx, p.Path)
	if err != nil {
		return m.block(d, "workspace_unavailable", "项目仓库目录不可读："+err.Error())
	}
	localMain, err := gitops.Head(m.ctx, p.Path, "refs/heads/"+d.Policy.Repository.MainBranch)
	if err != nil {
		return m.block(d, "main_branch_unavailable", "本地主分支不可读："+err.Error())
	}
	frozen, err := protectedDefinitionsFingerprint(m.ctx, p.Path, localMain, d.Policy.Checks)
	if err != nil {
		return m.block(d, "protected_definitions_unavailable", err.Error())
	}
	if d.ProtectedFingerprint != "" && d.ProtectedFingerprint != frozen {
		return m.block(d, "protected_definitions_changed", "派发前主分支检查定义已变化，请人工审核")
	}
	d.ProtectedFingerprint = frozen
	var dispatch *workbench.DeliveryStep
	if !m.dispatchedBefore(d.ID) {
		dispatch = &workbench.DeliveryStep{
			ID:               stepID(),
			DeliveryID:       d.ID,
			Kind:             "dispatch",
			InputFingerprint: "round:" + d.RoundID,
			Phase:            "running",
			Summary:          "准备派发原 pi 会话",
		}
		seeded := d
		sent, err := m.store.AdvanceDelivery(d.ID, d.Version, seeded, dispatch)
		if err != nil {
			return d, false
		}
		if err = m.cfg.Agent.Send(t.ConversationID, d.RoundID); err != nil {
			// sent is the version after the durable intent. Blocking with the
			// old d loses CAS and the next scan can invent developing without
			// ever sending a request.
			return m.block(sent, "dispatch_failed", "通知原会话失败，未标记为已派发："+err.Error())
		}
		m.rememberDispatched(d.ID)
		dispatch.Phase = "observed"
		dispatch.Summary = "已通知原 pi 会话执行本次任务"
		// Same staged chain still holds the store's own version, so the
		// closing persist below stays CAS-consistent.
		d = sent
	}
	next := d
	next.Phase = "developing"
	next.CommonDir = common
	next.Fingerprint = workbench.ProtectedDeliveryFingerprint(d.Policy)
	next.BaseMainSHA = x.BaseSHA
	if next.BaseMainSHA == "" {
		next.BaseMainSHA = localMain
	}
	next.LocalMainSHA = localMain
	next.TriggerDeadline = m.now().Add(dms(d.Policy.TriggerSeconds))
	next.ActiveSince = m.now()
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, dispatch)
	if err != nil {
		return next, false
	}
	return out, true
}

// stageDeveloping advances to local checking once the cycle's round has
// definitely settled; dispatching already happened in the queued stage.
func (m *Manager) stageDeveloping(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	if _, _, _, ok := executionOf(st, d); !ok {
		return m.block(d, "workspace_missing", "任务已不存在")
	}
	settled, found := m.roundSettled(st, d)
	if !found {
		return m.block(d, "round_missing", "找不到周期对应的任务轮次")
	}
	if !settled {
		return d, false
	}
	next := d
	next.Phase = "local_checking"
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	return out, true
}

// stageLocalChecking acquires the task write lease, runs bounded local
// checks and freezes the candidate. Any unknown commit outcome parks the
// cycle instead of retrying.
func (m *Manager) stageLocalChecking(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	p, t, x, ok := executionOf(st, d)
	if !ok || x.Workspace == "" {
		return m.block(d, "workspace_missing", "任务执行缺少可用工作区")
	}
	lease := m.leaseFor(d.ID)
	if lease == nil {
		acquired, err := m.cfg.Agent.AcquireDeliveryLease(m.ctx, d.ID, "task")
		if err != nil {
			// Writer busy is transient: the owner lock is taken, not broken.
			return d, false
		}
		m.holdLease(d.ID, acquired)
		lease = acquired
	}
	cand, err := gitops.CaptureCandidate(m.ctx, p, t, x, m.cfg.DataDir)
	if err != nil {
		return m.block(d, "candidate_unavailable", "任务候选无法冻结："+err.Error())
	}
	if code, reason := m.verifyWorkflowTree(d, x.Workspace, cand.Tree); code != "" {
		return m.block(d, code, reason)
	}
	ev, err := RunChecks(m.ctx, x, d.Policy.Checks, lease)
	if leaseRefused(err) {
		return m.block(d, "writer_busy", "写入租约拒绝运行检查")
	}
	// Even failed checks may alter their definitions or produce special files.
	// Never repair automatically before the final protected tree is verified.
	if code, reason := m.verifyDefinitionsAfterChecks(d, x.Workspace, ev.Tree); code != "" {
		return m.block(d, code, reason)
	}
	if err != nil || !ev.Passed {
		// A failed local gate enters the same bounded repair loop as CI; the
		// failed candidate stays the reference, a direct push never happens.
		if err != nil {
			ev.Summary = clipRunes(ev.Summary+"\n环境失败："+err.Error(), workbench.RepairSummaryLimit)
		}
		return m.repair(d, githubactions.FailureEvidence{Code: "failure", Fingerprint: ev.Fingerprint}, &ev)
	}
	next := d
	next.Phase = "committing"
	next.CandidateSHA = cand.HEAD
	next.CandidateTree = cand.Tree
	if len(cand.Files) > 0 {
		// Committing without valid change would create an empty commit;
		// the candidate SHA stays the current HEAD and flows straight to
		// the same verified push.
		sha, err := gitops.CommitCandidate(m.ctx, x, cand, commitMessage(d), p.CommitName, p.CommitEmail, gitRunnerFunc)
		if err != nil {
			return m.block(d, "commit_unverified", "候选提交未确认，已保留工作区："+err.Error())
		}
		next.CandidateSHA = sha
		next.CandidateTree = cand.Tree
	}
	check := workbench.DeliveryStep{
		ID:               stepID(),
		DeliveryID:       d.ID,
		Kind:             "local_checks",
		InputFingerprint: cand.Fingerprint,
		Phase:            "observed",
		BeforeSHA:        cand.HEAD,
		AfterSHA:         next.CandidateSHA,
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

// stageCommitting re-verifies that the frozen candidate still heads the
// workspace before the task branch write is allowed to proceed.
func (m *Manager) stageCommitting(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	_, _, x, ok := executionOf(st, d)
	if !ok || x.Workspace == "" {
		return m.block(d, "workspace_missing", "任务执行缺少可用工作区")
	}
	head, err := gitops.Head(m.ctx, x.Workspace, "HEAD")
	if err != nil {
		return m.block(d, "candidate_unavailable", "任务工作区不可读："+err.Error())
	}
	if head != d.CandidateSHA {
		return m.block(d, "candidate_changed", "任务提交与已核实候选不一致，请人工复核")
	}
	next := d
	next.Phase = "pushing_task"
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	return out, true
}

// stagePushingTask pushes exactly the verified candidate to the task branch,
// then arms the CI gate deadline and the acceptance token. A durable intent
// step is written before the irreversible remote write so a crash between the
// push and its receipt is recoverable from records instead of guesses.
func (m *Manager) stagePushingTask(d workbench.Delivery) (workbench.Delivery, bool) {
	st := m.store.Snapshot()
	_, _, x, ok := executionOf(st, d)
	if !ok || x.Workspace == "" {
		return m.block(d, "workspace_missing", "任务执行缺少可用工作区")
	}
	target := remoteTarget(d.Policy, d.TaskID)
	// A durable intent from a crashed process is resolved before the write is
	// repeated; only a provably unlanded intent proceeds to push.
	if out, handled, cont := m.resolvePendingIntent(d, "push_task"); handled {
		return out, cont
	}
	intent := workbench.DeliveryStep{
		ID:               stepID(),
		DeliveryID:       d.ID,
		Kind:             "push_task",
		InputFingerprint: d.CandidateSHA,
		Phase:            "running",
		Ref:              target.TaskRef,
		Summary:          "拟推送任务分支：" + target.TaskRef,
		StartedAt:        m.now(),
	}
	seeded, err := m.store.AdvanceDelivery(d.ID, d.Version, d, &intent)
	if err != nil {
		return d, false
	}
	d = seeded
	if crashAt("after-task-intent") {
		return d, false
	}
	sha, err := m.cfg.Remote.Push(m.ctx, x.Workspace, target, d.CandidateSHA, target.TaskRef, d.Policy.GitCredentialRef, m.leaseFor(d.ID))
	if err != nil {
		return m.block(d, "push_unverified", "任务分支推送未确认成功："+err.Error())
	}
	if crashAt("after-task-push") {
		return d, false
	}
	next := d
	next.Phase = "task_ci"
	next.CIDeadline = intent.StartedAt.Add(dms(d.Policy.CISeconds))
	if d.Policy.RequireAcceptance {
		// Every push mints a fresh token: a changed candidate invalidates the
		// previous acceptance instead of reusing a stale one.
		token, terr := randomToken()
		if terr != nil {
			return m.block(d, "token_failed", "验收令牌无法生成")
		}
		next.AcceptanceToken = token
	}
	intent.Phase = "observed"
	intent.AfterSHA = sha
	intent.ExitKnown = true
	intent.FinishedAt = m.now()
	intent.Summary = "已推送任务分支：" + target.TaskRef + " @ " + sha
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &intent)
	if err != nil {
		return d, false
	}
	if crashAt("after-task-receipt") {
		return out, false
	}
	m.dropLease(d.ID, true)
	return out, true
}

// stageTaskCI is passive: it observes the exact task gate, records changing
// evidence and only advances on a proven pass. Waiting never claims result
// and never writes a terminal state.
func (m *Manager) stageTaskCI(d workbench.Delivery) (workbench.Delivery, bool) {
	now := m.now()
	if !d.CIDeadline.IsZero() && now.After(d.CIDeadline) {
		return m.block(d, "ci_deadline", "任务 CI 门禁超出预算")
	}
	obs, err := m.cfg.Actions.Observe(m.ctx, m.taskGate(d))
	if err != nil {
		return m.block(d, "ci_protocol_error", "CI 观察协议错误："+err.Error())
	}
	sig := ciSignature(obs.Runs)
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
		// Both modes enter the integration lane: the acceptance gate decides
		// whether a human reviews the final candidate before the main write.
		next.Phase = "preparing_integration"
		ci := workbench.DeliveryStep{
			ID:               stepID(),
			DeliveryID:       d.ID,
			Kind:             "ci_task",
			InputFingerprint: d2.CandidateSHA,
			Phase:            "observed",
			BeforeSHA:        d2.CandidateSHA,
			Summary:          "任务分支 CI 门禁通过",
			ExitKnown:        true,
		}
		out, err := m.store.AdvanceDelivery(d.ID, d2.Version, next, &ci)
		if err != nil {
			return d2, false
		}
		return out, false
	case githubactions.StateFailed:
		d2, ok := save()
		if !ok {
			return d, false
		}
		// Task 3 appends ":not_repairable" on non-repairable conclusions;
		// classification works on the plain conclusion code.
		plain := obs.Code
		if i := strings.Index(plain, ":"); i > 0 {
			plain = plain[:i]
		}
		// Non-failure conclusions are classified from the observation code
		// alone; only a genuine source failure justifies fetching evidence.
		ev := githubactions.FailureEvidence{Code: plain}
		if plain == "failure" {
			fetched, ferr := m.cfg.Actions.Failure(m.ctx, m.taskGate(d2), obs)
			if ferr != nil {
				return m.block(d2, "evidence_unavailable", "失败证据不可用："+ferr.Error())
			}
			ev = fetched
		}
		if repairable, code := ClassifyFailure(ev, nil); !repairable {
			return m.block(d2, code, "失败原因不可自动修复，请人工复核")
		}
		return m.repair(d2, ev, nil)
	case githubactions.StateBlocked:
		return m.block(d, "ci_"+obs.Code, "任务分支 CI 门禁无法判定："+obs.Code)
	default:
		// Waiting: record changing evidence, never claim progress.
		if changed && len(obs.Runs) > 0 {
			if _, err := m.store.SaveDeliveryCI(d.ID, d.Version, obs.Runs); err == nil {
				m.rememberCI(d.ID, sig)
			}
		}
		return d, false
	}
}

// taskGate is the exact observation key of the task stage: only the
// authorized task workflows and the frozen candidate SHA count.
func (m *Manager) taskGate(d workbench.Delivery) githubactions.RunKey {
	return githubactions.RunKey{
		RepositoryID:  d.Policy.Repository.ID,
		DeliveryID:    d.ID,
		Stage:         "task",
		Owner:         d.Policy.Repository.Owner,
		Repo:          d.Policy.Repository.Name,
		Branch:        "kaiwu/task-" + d.TaskID,
		SHA:           d.CandidateSHA,
		CredentialRef: d.Policy.APICredentialRef,
		Required:      d.Policy.TaskWorkflows,
	}
}

func commitMessage(d workbench.Delivery) string {
	return fmt.Sprintf("自动化交付候选 %s（任务 %s 第 %d 周期）", d.ID, d.TaskID, d.Cycle)
}
