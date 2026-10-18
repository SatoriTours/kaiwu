package delivery

import (
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// ClassifyFailure decides whether a failure may enter the automatic repair
// loop. Local check failures are always repairable. A CI failure is only
// considered when the concrete source-code conclusion "failure" survives:
// permissions, secrets, rate limits, conflicts, timeouts, cancellations,
// approval requirements and every unknown cause stay blocked. The evidence
// itself is inert text; no AI self-report can open this gate. A redacted
// token marker is inert: the secret is gone once sanitization happened.
func ClassifyFailure(e githubactions.FailureEvidence, local *CheckEvidence) (repairable bool, code string) {
	if local != nil && !local.Passed {
		return true, "local_check_failed"
	}
	if e.Code != "failure" {
		return false, "ci_" + e.Code
	}
	summary := strings.ToLower(e.Summary)
	for _, p := range failureCategories {
		for _, pattern := range p.patterns {
			if strings.Contains(summary, strings.ToLower(pattern)) {
				return false, "ci_" + p.code
			}
		}
	}
	return true, e.Code
}

type failureCategory struct {
	patterns []string
	code     string
}

var failureCategories = []failureCategory{
	{[]string{"approval"}, "approval"},
	{[]string{"permission", "forbidden", "unauthorized", "401", "403", "access denied", "not allowed"}, "permission"},
	{[]string{"secret", "credential"}, "secret"},
	{[]string{"rate limit", "secondary rate", "abuse"}, "rate_limit"},
	{[]string{"conflict"}, "conflict"},
	{[]string{"timed out", "timeout"}, "timeout"},
	{[]string{"cancelled", "canceled"}, "cancelled"},
}

// repair hands one bounded failure evidence to the original conversation as
// an automatic repair job and parks the cycle in "repairing". It is the only
// path from a failed gate back to more work; a direct push is never issued.
func (m *Manager) repair(d workbench.Delivery, ev githubactions.FailureEvidence, local *CheckEvidence) (workbench.Delivery, bool) {
	return m.repairStage(d, ev, local, "")
}

// repairStage is repair with an explicit evidence stage (integration checks or
// the main CI gate), so the repair job and its audit trail name the real
// failing gate instead of assuming the task CI.
func (m *Manager) repairStage(d workbench.Delivery, ev githubactions.FailureEvidence, local *CheckEvidence, failedStage string) (workbench.Delivery, bool) {
	// A classification re-check stays defensive here: the caller pre-filters
	// but the loop never trusts a stale caller.
	if local == nil {
		if repairable, _ := ClassifyFailure(ev, nil); !repairable {
			return m.block(d, "ci_failure_blocked", "失败原因不可自动修复，请人工复核")
		}
	}
	now := m.now()
	if !d.ActiveSince.IsZero() && d.ActiveLimitSeconds > 0 &&
		now.After(d.ActiveSince.Add(time.Duration(d.ActiveLimitSeconds)*time.Second)) {
		return m.block(d, "repair_deadline", "激活周期已超出预算，请人工复核")
	}
	jobStage, fingerprint, summary := "task_ci", ev.Fingerprint, ev.Summary
	if failedStage != "" {
		jobStage = failedStage
	}
	if local != nil {
		jobStage, fingerprint, summary = "local_checks", local.Fingerprint, local.Summary
	}
	summary = clipRunes(summary, workbench.RepairSummaryLimit)
	job, err := m.store.PrepareDeliveryRepair(d.ID, d.Version, d.CandidateSHA, jobStage, fingerprint, summary)
	if err != nil {
		raw := err.Error()
		switch {
		case strings.Contains(err.Error(), "修复额度"):
			return m.block(d, "repair_budget", "修复额度已用完，请人工处理")
		case strings.Contains(err.Error(), "无新变更"):
			return m.block(d, "repair_loop", "相同失败且无新变更，停止自动修复")
		case strings.Contains(err.Error(), "交付阶段已变化"):
			// Another writer moved the cycle mid-decision; the next pass
			// re-evaluates the fresh state.
			return d, false
		default:
			return m.block(d, "repair_prepare_failed", "修复任务准备失败："+raw)
		}
	}
	if err := m.cfg.Agent.DispatchRepair(m.ctx, d.ID, job.ID); err != nil {
		return m.block(d, "repair_dispatch_failed", "修复派发失败："+err.Error())
	}
	// The claim charges the shared budget inside the store and bumps the
	// cycle version; the phase CAS must reuse the post-claim cycle itself,
	// not patch the stale copy with a new version — that would erase the
	// charged budget count and break the loop stop.
	fresh, err := m.store.Delivery(d.ID)
	if err != nil {
		return d, false
	}
	next := fresh
	next.Phase = "repairing"
	next.CIDeadline = time.Time{}
	out, err := m.store.AdvanceDelivery(d.ID, fresh.Version, next, nil)
	if err != nil {
		// The repair job is already prepared and possibly dispatched; the
		// next pass binds to it in stageRepairing instead of re-preparing.
		return d, false
	}
	return out, true
}

// stageRepairing binds the wait to the newest repair attempt only. Once its
// round (system_repair origin, message ID of the repair) has definitely
// finished, the cycle returns to local checking with cleared CI evidence —
// an old result is never trusted for a new push.
func (m *Manager) stageRepairing(d workbench.Delivery) (workbench.Delivery, bool) {
	job, err := m.store.LatestDeliveryRepair(d.ID)
	if err != nil {
		return m.block(d, "repair_unavailable", "修复任务不可读："+err.Error())
	}
	st := m.store.Snapshot()
	settled, found := m.roundStateFor(st, d.TaskID, job.MessageID)
	if !found || !settled {
		return d, false
	}
	next := d
	next.Phase = "local_checking"
	next.CIDeadline = time.Time{}
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	// Old CI evidence belongs to the failed candidate; forget the in-memory
	// signature so the next pass records fresh observations only.
	m.mu.Lock()
	delete(m.lastCI, d.ID)
	m.mu.Unlock()
	return out, true
}

// roundStateFor reports the settle state of one exact round id.
func (m *Manager) roundStateFor(st workbench.State, taskID, roundID string) (settled, found bool) {
	t, ok := st.Tasks[taskID]
	if !ok {
		return false, false
	}
	c, ok := st.Conversations[t.ConversationID]
	if !ok {
		return false, false
	}
	for _, r := range c.Rounds {
		if r.ID != roundID {
			continue
		}
		return r.Phase == "finished" || r.Phase == "confirmed", true
	}
	return false, false
}

// clipRunes bounds persisted evidence to the store's rune budget.
func clipRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
