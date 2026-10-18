package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

// Repair dispatch is the bounded loop that lets the original pi session fix a
// failed delivery stage. Prepare records supporting evidence only; claim is
// the atomic admission that charges the shared budget, appends a system round
// and hands the job to the agent port exactly once.
type repairRecord struct {
	Repair DeliveryRepair
	Prompt string
}

// Keeps the repair instruction inside one bounded prompt and leaves room for
// the fixed safety preamble.
const RepairSummaryLimit = 4000

const repairSummaryLimit = RepairSummaryLimit

// LatestDeliveryRepair returns the newest persisted repair job of one
// delivery, if any. The stage driver binds its wait to this attempt only;
// older attempts belong to their own rounds.
func (s *Store) LatestDeliveryRepair(deliveryID string) (DeliveryRepair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return DeliveryRepair{}, errors.New("store closed")
	}
	var body []byte
	err := s.db.QueryRow("SELECT body FROM delivery_repairs WHERE delivery_id=? ORDER BY number DESC LIMIT 1", deliveryID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryRepair{}, Missing()
	}
	if err != nil {
		return DeliveryRepair{}, err
	}
	var record repairRecord
	if err = json.Unmarshal(body, &record); err != nil {
		return DeliveryRepair{}, err
	}
	return record.Repair, nil
}

func repairPrompt(d Delivery, repair DeliveryRepair, summary string) string {
	return "自动修复（交付周期 " + d.ID + " 第 " + strconv.Itoa(repair.Number) + " 次，失败阶段 " + repair.Stage + "）：失败对象 SHA " + repair.FailedSHA + "；摘要：" + summary + "。请在原任务工作区修复失败原因，说明修改与实际验证；不要自动提交、合并、推送，不要修改检查定义或跳过任何门禁，不要触碰项目主分支。"
}

// repairRecords reads every persisted repair row of one delivery while
// Store.mu is held. List order is the audit ledger order.
func (s *Store) repairRecords(deliveryID string) ([]DeliveryRepair, error) {
	if s.closed {
		return nil, errors.New("store closed")
	}
	rows, err := s.db.Query("SELECT body FROM delivery_repairs WHERE delivery_id=? ORDER BY number", deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryRepair{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var record repairRecord
		if err = json.Unmarshal(body, &record); err != nil {
			return nil, err
		}
		out = append(out, record.Repair)
	}
	return out, rows.Err()
}

func (s *Store) repairRecordLocked(jobID string) (repairRecord, error) {
	var body []byte
	err := s.db.QueryRow("SELECT body FROM delivery_repairs WHERE id=?", jobID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return repairRecord{}, Missing()
	}
	if err != nil {
		return repairRecord{}, err
	}
	var record repairRecord
	if err = json.Unmarshal(body, &record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *Store) PrepareDeliveryRepair(deliveryID string, expectedVersion uint64, failedSHA, stage, failureFingerprint, summary string) (DeliveryRepair, error) {
	if !validText(failedSHA, 128) || !validText(stage, 40) || !validText(failureFingerprint, 200) || !validText(summary, repairSummaryLimit) {
		return DeliveryRepair{}, Invalid("修复失败证据无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return DeliveryRepair{}, errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return DeliveryRepair{}, err
	}
	// CAS on the frozen cycle: the caller proves it inspected exactly this
	// blocked state, so stale evidence cannot mint a repair job.
	if d.Version != expectedVersion {
		return DeliveryRepair{}, Conflict("交付阶段已变化")
	}
	if d.RepairsUsed >= d.RepairsLimit {
		return DeliveryRepair{}, Conflict("修复额度已用完，请人工处理")
	}
	repairs, err := s.repairRecords(deliveryID)
	if err != nil {
		return DeliveryRepair{}, err
	}
	// Same failure fingerprint on an unchanged candidate is a loop, not a
	// repair: stop here instead of burning budget on identical work.
	for _, r := range repairs {
		if r.FailureFingerprint == failureFingerprint && r.FailedSHA == d.CandidateSHA {
			return DeliveryRepair{}, Conflict("相同失败指纹且无新变更，停止自动修复")
		}
	}
	number := 0
	for _, r := range repairs {
		if r.Number > number {
			number = r.Number
		}
	}
	number++
	st := clone(s.state)
	t, ok := st.Tasks[d.TaskID]
	if !ok {
		return DeliveryRepair{}, Missing()
	}
	c, ok := st.Conversations[t.ConversationID]
	if !ok {
		return DeliveryRepair{}, Missing()
	}
	repair := DeliveryRepair{ID: id(), DeliveryID: d.ID, FailureFingerprint: failureFingerprint, FailedSHA: failedSHA, Stage: stage, Phase: "prepared", MessageID: id(), Number: number, Profile: roundProfile(c)}
	record := repairRecord{Repair: repair, Prompt: repairPrompt(d, repair, summary)}
	body, e := json.Marshal(record)
	if e != nil {
		return DeliveryRepair{}, e
	}
	// No private state change: revision still moves so observers can react.
	if err = s.persistWith(st, "", "", Receipt{}, func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO delivery_repairs(id,delivery_id,number,phase,body) VALUES(?,?,?,?,?)", repair.ID, d.ID, number, "prepared", body)
		return e
	}); err != nil {
		return DeliveryRepair{}, err
	}
	repair.Prompt = record.Prompt
	return repair, nil
}

// ClaimDeliveryRepair is the single-use admission for a prepared repair job:
// one transaction records the budget charge, the system message, the repair
// round and the claimed phase. A phase other than prepared means another
// dispatch already owns the job; ok=false carries the job back unchanged.
func (s *Store) ClaimDeliveryRepair(jobID string) (DeliveryRepair, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return DeliveryRepair{}, false, errors.New("store closed")
	}
	record, err := s.repairRecordLocked(jobID)
	if err != nil {
		return DeliveryRepair{}, false, err
	}
	if record.Repair.Phase != "prepared" {
		return record.Repair, false, nil
	}
	d, err := s.deliveryLocked(record.Repair.DeliveryID)
	if err != nil {
		return DeliveryRepair{}, false, err
	}
	if d.RepairsUsed >= d.RepairsLimit {
		return DeliveryRepair{}, false, Conflict("修复额度已用完，请人工处理")
	}
	next := clone(s.state)
	t, ok := next.Tasks[d.TaskID]
	if !ok {
		return DeliveryRepair{}, false, Missing()
	}
	c, ok := next.Conversations[t.ConversationID]
	if !ok {
		return DeliveryRepair{}, false, Missing()
	}
	if TaskMerging(next, t.ID) {
		return DeliveryRepair{}, false, Conflict("任务正在合并，请稍后再发送")
	}
	if c.RunState == "running" || c.RunState == "starting" {
		return DeliveryRepair{}, false, Conflict("会话正在运行，请等待当前轮次结束")
	}
	if len(c.Messages) >= 2000 {
		return DeliveryRepair{}, false, Invalid("会话消息已达上限")
	}
	d2 := d
	d2.RepairsUsed++
	d2.Version++
	profile := roundProfile(c)
	c.Messages = append(c.Messages, Message{ID: record.Repair.MessageID, Role: "system", Kind: "automatic_repair", Text: record.Prompt, Delivery: "pending"})
	c.RunState = "starting"
	c.Error = ""
	next.Conversations[c.ID] = c
	BeginRound(&next, c.ID, record.Repair.MessageID)
	UpdateRound(&next, c.ID, record.Repair.MessageID, func(r *Round) {
		r.Origin = "system_repair"
		r.DeliveryID = d.ID
		r.Profile = profile
	})
	claim := record
	claim.Repair.Phase = "claimed"
	claim.Repair.Profile = profile
	body, e := json.Marshal(claim)
	if e != nil {
		return DeliveryRepair{}, false, e
	}
	err = s.persistWith(next, "", "", Receipt{}, func(tx *sql.Tx) error {
		if e := updateDeliveryRow(tx, d2, d.Version); e != nil {
			return e
		}
		r, e := tx.Exec("UPDATE delivery_repairs SET phase='claimed',body=? WHERE id=? AND phase='prepared'", body, jobID)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return Conflict("修复任务已被其他实例处理")
		}
		return nil
	})
	if err != nil {
		return DeliveryRepair{}, false, err
	}
	claim.Repair.Prompt = record.Prompt
	return claim.Repair, true, nil
}

// MarkRepairDispatching records that the agent port is about to call the
// model for this claimed job. Bookkeeping before every Call keeps process
// death and restarts analyzable instead of guessed.
func (s *Store) MarkRepairDispatching(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	claims, err := s.repairRecordLocked(jobID)
	if err != nil {
		return err
	}
	if claims.Repair.Phase != "claimed" {
		return Conflict("修复任务状态已变化，不能重复派发")
	}
	claims.Repair.Phase = "dispatching"
	body, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE delivery_repairs SET phase='dispatching',body=? WHERE id=? AND phase='claimed'", body, jobID)
	return err
}

// RequestNextProfile asks for a model change that becomes effective at the
// next round boundary, never inside a running request, and never rewrites
// the server default or any other task's profile.
func (s *Store) RequestNextProfile(operationID, conversationID string, profile Profile) (Receipt, error) {
	if !validText(profile.Provider, 200) || !validText(profile.Model, 300) {
		return Receipt{}, Invalid("请从 pi 检测结果选择模型")
	}
	switch profile.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return Receipt{}, Invalid("思考级别无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Receipt{}, errors.New("store closed")
	}
	profile.CredentialSource = "server_pi"
	fp := deliveryFingerprint(struct {
		Kind string
		Conv string
		Next Profile
	}{"request_next_profile", conversationID, profile})
	if _, ok := s.state.Conversations[conversationID]; !ok {
		return Receipt{}, Missing()
	}
	if r, ok, err := s.deliveryOperation(operationID, fp); ok || err != nil {
		return r, err
	}
	next := clone(s.state)
	c := next.Conversations[conversationID]
	c.PendingProfile = &profile
	next.Conversations[conversationID] = c
	next.StructureRevision++
	r := Receipt{Type: "request_next_profile", ConversationID: conversationID}
	if err := s.persistWith(next, operationID, fp, r, nil); err != nil {
		return Receipt{}, err
	}
	return r, nil
}
