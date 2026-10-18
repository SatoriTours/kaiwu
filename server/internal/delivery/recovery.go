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
	page, err := m.store.DeliverySteps(d.ID, 0, 100)
	if err != nil {
		return d, false, false
	}
	for _, s := range page.Items {
		if s.Kind != kind || s.Phase != "running" {
			continue
		}
		rec, err := m.ReconcileStep(m.ctx, d, s)
		if err != nil {
			return d, false, false
		}
		switch rec.State {
		case "observed":
			restored, err := m.backfillObserved(d, s, rec)
			if err != nil {
				return d, false, false
			}
			return restored, true, true
		case "not_started":
			// Provably no write happened; the stage may proceed.
			return d, false, false
		default:
			blocked, _ := m.block(d, "recovery_uncertain", rec.Reason)
			return blocked, true, false
		}
	}
	return d, false, false
}

// Recover reconciles every active cycle before the service accepts new work.
// It is read-only except for uniquely proven backfills.
func (m *Manager) Recover(ctx context.Context) error {
	list, err := m.store.ActiveDeliveries()
	if err != nil {
		return err
	}
	for _, d := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.recoverOne(ctx, d); err != nil {
			// A conflicting write means another writer moved the cycle while
			// we reconciled; that cycle re-evaluates itself, recovery does not
			// force a lost update.
			var we *workbench.Error
			if errors.As(err, &we) && we.Code == "conflict" {
				continue
			}
			return err
		}
	}
	return nil
}

// recoverOne resolves the interrupted windows of one cycle. A live gate
// failure (revoked authorization, changed policy) blocks instead of resuming.
func (m *Manager) recoverOne(ctx context.Context, d workbench.Delivery) error {
	if code, reason := m.gateCode(d); code != "" {
		_, _ = m.block(d, code, reason)
		return nil
	}
	page, err := m.store.DeliverySteps(d.ID, 0, 100)
	if err != nil {
		return err
	}
	for _, step := range page.Items {
		if step.Phase != "running" {
			continue
		}
		rec, err := m.ReconcileStep(ctx, d, step)
		if err != nil {
			return err
		}
		switch rec.State {
		case "observed":
			if _, err := m.backfillObserved(d, step, rec); err != nil {
				return err
			}
		case "not_started":
			// The effect provably did not happen: record that and let the
			// owning stage run again on the next pass.
			step.Phase = "intent"
			step.Summary = "已核实未产生写入，交回原阶段重试：" + rec.Reason
			if _, err := m.store.AdvanceDelivery(d.ID, d.Version, d, &step); err != nil {
				return err
			}
		default:
			// Unknown windows are never resolved by guessing.
			next := d
			next.Phase = "needs_check"
			next.BlockCode = "recovery_uncertain"
			next.BlockReason = rec.Reason
			step.Phase = "uncertain"
			step.Summary = "结果未知：" + rec.Reason
			if _, err := m.store.AdvanceDelivery(d.ID, d.Version, next, &step); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
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
	step.Phase = "observed"
	step.AfterSHA = rec.AfterSHA
	step.ExitKnown = true
	step.FinishedAt = m.now()
	step.Summary = step.Summary + "（重启恢复补记：" + rec.Reason + "）"
	return m.store.AdvanceDelivery(d.ID, d.Version, next, &step)
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
