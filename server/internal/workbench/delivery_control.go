package workbench

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Acceptance and control are user operations on one delivery cycle. Both
// persist the original operation receipt inside the same transaction as the
// delivery row change, so replays return the same receipt and can never
// re-execute the mutation.

// AcceptDelivery records the human acceptance of one pushed candidate. The
// acceptance token is bound to exactly this cycle: correct token, exact
// version, awaiting phase, non-empty candidate identity. The stored result
// keeps the historical acceptance visible afterwards; the machine never
// confirms the task result itself.
func (s *Store) AcceptDelivery(operationID, deliveryID, token string, expectedVersion uint64) (Receipt, error) {
	if operationID == "" {
		return Receipt{}, Invalid("操作标识无效")
	}
	if token == "" {
		return Receipt{}, Invalid("请提供验收令牌")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return Receipt{}, err
	}
	// The fingerprint covers the target token and version so a stale caller
	// cannot replay an acceptance computed for an older cycle state.
	fp := deliveryFingerprint(struct {
		Kind      string
		Delivery  string
		Token     string
		VersionGC uint64
	}{"delivery_accept", deliveryID, token, expectedVersion})
	if r, ok, err := s.deliveryOperation(operationID, fp); ok || err != nil {
		return r, err
	}
	if d.Version != expectedVersion {
		return Receipt{}, Conflict("交付阶段已变化")
	}
	if d.Phase != "awaiting_acceptance" {
		return Receipt{}, Conflict("当前阶段不能验收，请先完成验收门禁")
	}
	if token != d.AcceptanceToken {
		return Receipt{}, Conflict("验收令牌无效，请使用最新周期的令牌")
	}
	if d.CandidateSHA == "" || d.CandidateTree == "" || d.RoundID == "" {
		return Receipt{}, Invalid("候选身份不完整，禁止验收")
	}
	next := d
	next.Phase = "integrating"
	next.AcceptedSHA = d.CandidateSHA
	next.Version++
	next.FinishedAt = time.Now().UTC()
	summary, err := s.deliverySummary(next, nil, nil)
	if err != nil {
		return Receipt{}, err
	}
	state := clone(s.state)
	state.Deliveries[next.TaskID] = summary
	r := Receipt{Type: "delivery_accept", DeliveryID: d.ID, CreatedID: next.AcceptedSHA}
	// Acceptance admits the verified candidate to the local main fast-forward;
	// the integration slot is released only after the main CI gate passes.
	err = s.persistWith(state, operationID, fp, r, func(tx *sql.Tx) error {
		return updateDeliveryRow(tx, next, d.Version)
	})
	if err != nil {
		return Receipt{}, err
	}
	return r, nil
}

// ControlDelivery applies one explicit pause/resume/cancel/extend_budget
// action to one delivery cycle. Control actions never take an acceptance
// token: only Accept consumes one. extend_budget appends exactly one policy
// snapshot allowance (default 3 repairs / 3600s) with an audit step; history
// counters and the frozen policy snapshot stay untouched.
func (s *Store) ControlDelivery(operationID, deliveryID, action, token string, expectedVersion uint64) (Receipt, error) {
	switch action {
	case "pause", "resume", "cancel", "extend_budget":
	default:
		return Receipt{}, Invalid("不支持的控制动作，可选 pause/resume/cancel/extend_budget")
	}
	if token != "" {
		return Receipt{}, Invalid("控制操作不接受验收令牌")
	}
	if operationID == "" {
		return Receipt{}, Invalid("操作标识无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return Receipt{}, err
	}
	fp := deliveryFingerprint(struct {
		Kind     string
		Delivery string
		Action   string
	}{"delivery_control", deliveryID, action})
	if r, ok, err := s.deliveryOperation(operationID, fp); ok || err != nil {
		return r, err
	}
	if d.Version != expectedVersion {
		return Receipt{}, Conflict("交付阶段已变化")
	}
	if !DeliveryActive(d.Phase) {
		return Receipt{}, Conflict("交付周期已结束")
	}
	next := d
	next.Version++
	var step *DeliveryStep
	releaseQueue := false
	switch action {
	case "pause":
		if d.Phase == "blocked" || d.Phase == "paused" {
			return Receipt{}, Conflict("当前阶段不能暂停")
		}
		next.Phase = "paused"
	case "resume":
		if d.Phase != "paused" {
			return Receipt{}, Conflict("只有暂停中的周期可以恢复")
		}
		next.Phase = "local_checking"
		next.CIDeadline = time.Time{}
	case "cancel":
		next.Phase = "cancelled"
		next.FinishedAt = time.Now().UTC()
		// Cancel is an explicit stop that admits the cycle never passed
		// verification: it releases the integration lane with a warning step.
		step = &DeliveryStep{
			ID:               id(),
			DeliveryID:       d.ID,
			Kind:             "release_unverified",
			InputFingerprint: "release:cancel",
			Phase:            "observed",
			Summary:          "取消交付并解除隔离：未完成核验，未验证警告保留",
			ExitKnown:        true,
		}
		releaseQueue = true
	case "extend_budget":
		next.RepairsLimit = d.RepairsLimit + d.Policy.MaxRepairs
		next.ActiveLimitSeconds = d.ActiveLimitSeconds + d.Policy.ActiveSeconds
		step = &DeliveryStep{
			ID:               id(),
			DeliveryID:       d.ID,
			Kind:             "extend_budget",
			InputFingerprint: fmt.Sprintf("budget:%d:%d", d.RepairsLimit, d.ActiveLimitSeconds),
			Phase:            "observed",
			Summary:          fmt.Sprintf("追加修复额度：+%d 次 / +%d 秒，仅一次授权", d.Policy.MaxRepairs, d.Policy.ActiveSeconds),
			ExitKnown:        true,
		}
	}
	writeStep, prepared, err := s.prepareDeliveryStep(deliveryID, step)
	if err != nil {
		return Receipt{}, err
	}
	summary, err := s.deliverySummary(next, prepared, nil)
	if err != nil {
		return Receipt{}, err
	}
	state := clone(s.state)
	state.Deliveries[next.TaskID] = summary
	r := Receipt{Type: "delivery_control", DeliveryID: d.ID, Notice: action}
	err = s.persistWith(state, operationID, fp, r, func(tx *sql.Tx) error {
		if writeStep != nil {
			if e := writeStep(tx); e != nil {
				return e
			}
		}
		if releaseQueue {
			if _, e := tx.Exec("DELETE FROM delivery_queue WHERE delivery_id=?", d.ID); e != nil {
				return e
			}
		}
		return updateDeliveryRow(tx, next, d.Version)
	})
	if err != nil {
		return Receipt{}, err
	}
	return r, nil
}
