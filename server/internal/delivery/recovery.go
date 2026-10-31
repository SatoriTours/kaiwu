package delivery

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Recovery reconciles persisted steps whose outcome is unknown after a crash
// or restart. It never guesses: a step is only backfilled when the durable
// record plus a read-only observation prove exactly what happened. Anything
// ambiguous parks the cycle in needs_check and keeps every logical slot.

// crashAt is the test-only interruption hook. The shipped default never
// stops mid-window; tests store a point via crashPoint.
var crashPoint atomic.Value

func crashAt(point string) bool {
	v, _ := crashPoint.Load().(string)
	return v != "" && v == point
}

// setCrashPoint arms one interruption point for the next machine pass.
func setCrashPoint(point string) { crashPoint.Store(point) }

// clearCrashPoint disarms the interruption hook.
func clearCrashPoint() { crashPoint.Store("") }

// Reconciliation is the verdict for one interrupted step.
type Reconciliation struct {
	State      string // observed | not_started | uncertain
	AfterSHA   string
	ResumeSafe bool
	Reason     string
}

// resolvePendingIntent reconciles a durable intent left by a crash before a
// stage repeats an irreversible write. handled=true means the stage must not
// perform the write again; cont is the paced-chain continuation flag.
func (m *Manager) resolvePendingIntent(d workbench.Delivery, kind string) (out workbench.Delivery, handled, cont bool) {
	steps, err := m.pendingSteps(d.ID)
	if err != nil {
		return d, true, false
	}
	type observation struct {
		step workbench.DeliveryStep
		rec  Reconciliation
	}
	var batch []observation
	anchor, uncertain := -1, -1
	// Classify the entire matching set before advancing the cycle. An earlier
	// absent/observed effect never grants permission to skip a later unknown.
	for _, s := range steps {
		if s.Kind != kind {
			continue
		}
		rec, err := m.ReconcileStep(m.ctx, d, s)
		if err != nil {
			return d, true, false
		}
		i := len(batch)
		batch = append(batch, observation{s, rec})
		switch rec.State {
		case "observed":
			if anchor < 0 || s.StartedAt.Before(batch[anchor].step.StartedAt) {
				anchor = i
			}
		case "not_started":
		default:
			if uncertain < 0 {
				uncertain = i
			}
		}
	}
	if len(batch) == 0 {
		return d, false, false
	}
	for i, o := range batch {
		// Keep one proven receipt running until its atomic phase+receipt CAS.
		// A crash midway must not leave pushing_* with no pending evidence and
		// consequently permit a duplicate push. Unknowns prohibit that advance.
		if i == anchor && uncertain < 0 {
			continue
		}
		s := o.step
		switch o.rec.State {
		case "not_started":
			s.Phase = "intent"
			s.Summary = "已核实未产生写入，交回原阶段重试：" + o.rec.Reason
		case "observed":
			s = m.observedReceipt(s, o.rec)
		default:
			continue
		}
		updated, err := m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
		if err != nil {
			return d, true, false
		}
		d = updated
	}
	if uncertain >= 0 {
		for _, o := range batch {
			if o.rec.State == "observed" || o.rec.State == "not_started" {
				continue
			}
			parked, err := m.parkUncertain(d, o.step, o.rec.Reason)
			if err != nil {
				return d, true, false
			}
			d = parked
		}
		return d, true, false
	}
	if anchor >= 0 {
		o := batch[anchor]
		restored, err := m.backfillObserved(d, o.step, o.rec)
		if err != nil {
			return d, true, false
		}
		return restored, true, true
	}
	// All writes were proven absent and durably recorded. Retry only on a
	// fresh stage pass, with the new version and all normal gates rechecked.
	return d, true, true
}

// Recover reconciles every active cycle before the service accepts new work.
// It is read-only except for uniquely proven backfills.
func (m *Manager) Recover(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	if err := m.acquirePass(ctx); err != nil {
		return err
	}
	defer m.releasePass()
	list, err := m.store.ActiveDeliveries()
	if err != nil {
		return err
	}
	for _, d := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.recoverOne(ctx, d); err != nil {
			// Even a CAS conflict leaves this snapshot unverified. In particular
			// initial recovery must not open scheduling after skipping a cycle.
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.readyOnce.Do(func() { close(m.ready) })
	m.Wake()
	return nil
}

// recoverOne resolves the interrupted windows of one cycle. A live gate
// failure (revoked authorization, changed policy) blocks instead of resuming.
func (m *Manager) recoverOne(ctx context.Context, d workbench.Delivery) error {
	if code, reason := m.gateCode(d); code != "" {
		next := d
		next.Phase, next.BlockCode, next.BlockReason = "blocked", code, reason
		_, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
		if err == nil {
			m.dropLease(d.ID, false)
		}
		return err
	}
	// A persisted manual-check decision survives repeated startup recovery.
	// If a batch was interrupted while parking several unknowns, a later
	// observation for another row cannot erase the earlier unresolved result.
	if d.Phase == "needs_check" && d.BlockCode == "recovery_uncertain" {
		return nil
	}
	steps, err := m.pendingSteps(d.ID)
	if err != nil {
		return err
	}
	for _, step := range steps {
		if step.Phase != "running" {
			continue
		}
		rec, err := m.ReconcileStep(ctx, d, step)
		if err != nil {
			return err
		}
		switch rec.State {
		case "observed":
			d, err = m.backfillObserved(d, step, rec)
			if err != nil {
				return err
			}
		case "not_started":
			// The effect provably did not happen: record that and let the
			// owning stage run again on the next pass.
			step.Phase = "intent"
			step.Summary = "已核实未产生写入，交回原阶段重试：" + rec.Reason
			d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &step)
			if err != nil {
				return err
			}
		default:
			_, err := m.parkUncertain(d, step, rec.Reason)
			return err
		}
	}
	return nil
}

// pendingSteps reads complete bounded history, not only the first page.
// If the history cannot be completely inspected, no caller may repeat a write.
func (m *Manager) pendingSteps(id string) ([]workbench.DeliveryStep, error) {
	var running []workbench.DeliveryStep
	var after uint64
	for pageNo := 0; pageNo < 64; pageNo++ {
		page, err := m.store.DeliverySteps(id, after, 100)
		if err != nil {
			return nil, err
		}
		for _, step := range page.Items {
			if step.Phase == "running" {
				running = append(running, step)
			}
		}
		if page.Next == 0 {
			return running, nil
		}
		if page.Next <= after {
			return nil, errors.New("恢复步骤分页未推进")
		}
		after = page.Next
	}
	return nil, errors.New("恢复步骤历史超过安全核查上限，请人工审核")
}

func (m *Manager) parkUncertain(d workbench.Delivery, step workbench.DeliveryStep, reason string) (workbench.Delivery, error) {
	next := d
	next.Phase, next.BlockCode, next.BlockReason = "needs_check", "recovery_uncertain", reason
	step.Phase, step.Summary = "uncertain", "结果未知："+reason
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &step)
	if err == nil {
		m.dropLease(d.ID, false)
	}
	return out, err
}

// ReconcileStep decides one interrupted step from records plus a read-only
// observation of the remote target.
func (m *Manager) ReconcileStep(ctx context.Context, d workbench.Delivery, step workbench.DeliveryStep) (Reconciliation, error) {
	switch step.Kind {
	case "push_task", "push_main":
		st := m.store.Snapshot()
		_, _, x, ok := executionOf(st, d)
		dir := ""
		if ok {
			dir = x.Workspace
		}
		ref := step.Ref
		if ref == "" {
			if step.Kind == "push_main" {
				ref = mainRef(d)
			} else {
				ref = "refs/heads/kaiwu/task-" + d.TaskID
			}
		}
		sha, err := m.cfg.Remote.Observe(ctx, dir, remoteTarget(d.Policy, d.TaskID), ref, d.Policy.GitCredentialRef, nil)
		if err != nil {
			return Reconciliation{State: "uncertain", Reason: "远程目标不可读，无法证明写入结果：" + err.Error()}, nil
		}
		switch {
		case step.InputFingerprint != "" && sha == step.InputFingerprint:
			return Reconciliation{State: "observed", AfterSHA: sha, ResumeSafe: true, Reason: "远程已指向本步骤拟推送的提交"}, nil
		case sha == step.BeforeSHA:
			return Reconciliation{State: "not_started", ResumeSafe: true, Reason: "远程仍为本步骤前的基线"}, nil
		default:
			return Reconciliation{State: "uncertain", Reason: "远程既非拟推送提交也非原基线，可能有其他写入者"}, nil
		}
	case "commit":
		// A commit is only backfilled when the workspace itself proves it: the
		// recorded result is HEAD and its first parent is the recorded base.
		st := m.store.Snapshot()
		_, _, x, ok := executionOf(st, d)
		if !ok || x.Workspace == "" {
			return Reconciliation{State: "uncertain", Reason: "任务工作区不可读，提交结果未知"}, nil
		}
		if step.AfterSHA == "" || step.BeforeSHA == "" {
			return Reconciliation{State: "uncertain", Reason: "提交窗口缺少 parent 或结果记录"}, nil
		}
		head, err := gitHead(m.ctx, x.Workspace)
		if err != nil {
			return Reconciliation{State: "uncertain", Reason: "任务工作区不可读，提交结果未知"}, nil
		}
		if head != step.AfterSHA {
			return Reconciliation{State: "not_started", ResumeSafe: true, Reason: "工作区未到达拟提交状态"}, nil
		}
		parent, err := gitParent(m.ctx, x.Workspace)
		if err != nil || parent != step.BeforeSHA {
			return Reconciliation{State: "uncertain", Reason: "提交父子关系无法唯一证明"}, nil
		}
		return Reconciliation{State: "observed", AfterSHA: head, ResumeSafe: true, Reason: "工作区提交与记录的唯一父子关系一致"}, nil
	default:
		return Reconciliation{State: "uncertain", Reason: "未知步骤窗口不能自动补记：" + step.Kind}, nil
	}
}

// backfillObserved writes the missing receipt and moves the cycle to the phase
// the completed effect implies, keeping the original deadline (an interrupted
// CI window must not silently gain fresh budget).
func (m *Manager) backfillObserved(d workbench.Delivery, step workbench.DeliveryStep, rec Reconciliation) (workbench.Delivery, error) {
	next := d
	switch step.Kind {
	case "push_task":
		next.Phase = "task_ci"
		next.CIDeadline = step.StartedAt.Add(dms(d.Policy.CISeconds))
		if d.Policy.RequireAcceptance && next.AcceptanceToken == "" {
			token, err := randomToken()
			if err != nil {
				return d, err
			}
			next.AcceptanceToken = token
		}
	case "push_main":
		next.Phase = "main_ci"
		next.CIDeadline = step.StartedAt.Add(dms(d.Policy.CISeconds))
	case "commit":
		next.CandidateSHA = rec.AfterSHA
	default:
		return d, nil
	}
	// Duplicate observed records cannot extend a CI window already restored
	// by an earlier same-stage intent. A different CI stage gets its own budget.
	if (step.Kind == "push_task" || step.Kind == "push_main") &&
		d.Phase == next.Phase && !d.CIDeadline.IsZero() && d.CIDeadline.Before(next.CIDeadline) {
		next.CIDeadline = d.CIDeadline
	}
	step = m.observedReceipt(step, rec)
	return m.store.AdvanceDelivery(d.ID, d.Version, next, &step)
}

func (m *Manager) observedReceipt(step workbench.DeliveryStep, rec Reconciliation) workbench.DeliveryStep {
	step.Phase = "observed"
	step.AfterSHA = rec.AfterSHA
	step.ExitKnown = true
	step.FinishedAt = m.now()
	step.Summary += "（重启恢复补记：" + rec.Reason + "）"
	return step
}

// gitHead/gitParent are tiny wrappers so recovery can prove commit windows.
func gitHead(ctx context.Context, dir string) (string, error) {
	out, err := gitops.Run(ctx, dir, nil, "rev-parse", "--verify", "HEAD^{commit}")
	return strings.TrimSpace(out), err
}

func gitParent(ctx context.Context, dir string) (string, error) {
	out, err := gitops.Run(ctx, dir, nil, "rev-parse", "--verify", "HEAD^1^{commit}")
	return strings.TrimSpace(out), err
}
