package workbench

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

func deliveryFingerprint(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Store) deliveryOperation(key, fp string) (Receipt, bool, error) {
	if s.closed {
		return Receipt{}, false, errors.New("store closed")
	}
	if len(key) < 3 || len(key) > 100 {
		return Receipt{}, false, Invalid("操作标识无效")
	}
	var previous string
	var body []byte
	err := s.db.QueryRow("SELECT fingerprint,receipt FROM operations WHERE id=?", key).Scan(&previous, &body)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM operations").Scan(&count); err != nil {
			return Receipt{}, false, err
		}
		if count >= 10000 {
			return Receipt{}, false, Invalid("操作记录已达当前版本上限")
		}
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	if previous != fp {
		return Receipt{}, false, Conflict("操作标识已用于不同内容")
	}
	var r Receipt
	err = json.Unmarshal(body, &r)
	r.Replay = true
	return r, true, err
}
func (s *Store) deliveryPolicyLocked(projectID string) (ProjectDeliveryPolicy, error) {
	if s.closed {
		return ProjectDeliveryPolicy{}, errors.New("store closed")
	}
	if _, ok := s.state.Projects[projectID]; !ok {
		return ProjectDeliveryPolicy{}, Missing()
	}
	var body []byte
	err := s.db.QueryRow("SELECT body FROM delivery_policies WHERE project_id=?", projectID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultDeliveryPolicy(projectID), nil
	}
	var p ProjectDeliveryPolicy
	if err == nil {
		err = json.Unmarshal(body, &p)
	}
	return p, err
}
func (s *Store) DeliveryPolicy(projectID string) (ProjectDeliveryPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryPolicyLocked(projectID)
}
func (s *Store) SaveDeliveryPolicy(operationID string, expectedVersion uint64, p ProjectDeliveryPolicy) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp := deliveryFingerprint(struct {
		Kind    string
		Version uint64
		Policy  ProjectDeliveryPolicy
	}{"delivery_policy", expectedVersion, p})
	if r, ok, err := s.deliveryOperation(operationID, fp); ok || err != nil {
		return r, err
	}
	current, err := s.deliveryPolicyLocked(p.ProjectID)
	if err != nil {
		return Receipt{}, err
	}
	if current.Version != expectedVersion {
		return Receipt{}, Conflict("项目交付配置已变化")
	}
	if err = ValidateDeliveryPolicy(p); err != nil {
		return Receipt{}, err
	}
	p.Version = current.Version + 1
	if !p.Enabled {
		p.RevokedAt = time.Now().UTC()
	}
	next := clone(s.state)
	next.DeliveryPolicies[p.ProjectID] = DeliveryPolicySummary{Version: p.Version, Enabled: p.Enabled, RequireAcceptance: p.RequireAcceptance, Repository: p.Repository}
	next.StructureRevision++
	r := Receipt{Type: "delivery_policy", CreatedID: p.ProjectID, Revision: next.Revision + 1}
	body, _ := json.Marshal(p)
	err = s.persistWith(next, operationID, fp, r, func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO delivery_policies(project_id,version,body) VALUES(?,?,?) ON CONFLICT(project_id) DO UPDATE SET version=excluded.version,body=excluded.body", p.ProjectID, p.Version, body)
		return e
	})
	return r, err
}
func (s *Store) deliveryLocked(key string) (Delivery, error) {
	if s.closed {
		return Delivery{}, errors.New("store closed")
	}
	var body []byte
	err := s.db.QueryRow("SELECT body FROM deliveries WHERE id=?", key).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, Missing()
	}
	var d Delivery
	if err == nil {
		err = json.Unmarshal(body, &d)
	}
	return d, err
}
func (s *Store) Delivery(key string) (Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryLocked(key)
}
func (s *Store) ActiveDeliveries() ([]Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("store closed")
	}
	rows, err := s.db.Query("SELECT body FROM deliveries WHERE active=1 ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var body []byte
		var d Delivery
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Called under Store.mu. The returned closure performs SQL only, inside the
// same transaction as the user message, round, projection and operation receipt.
func (s *Store) prepareDeliveryStart(next *State, c Command, r *Receipt) (func(*sql.Tx) error, error) {
	p, err := s.deliveryPolicyLocked(c.ProjectID)
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, nil
	}
	if err = ValidateDeliveryPolicy(p); err != nil {
		return nil, err
	}
	task := next.Tasks[c.TaskID]
	// A task mid-merge may not open a delivery cycle that would race the same
	// main branch; the merge must finish or be abandoned first.
	if TaskMerging(*next, task.ID) {
		return nil, Conflict("任务正在合并，请结束后再开始交付")
	}
	summary := s.state.Deliveries[task.ID]
	var d Delivery
	if summary.ID != "" && DeliveryActive(summary.Phase) {
		d, err = s.deliveryLocked(summary.ID)
		if err != nil {
			return nil, err
		}
		if d.Phase != "developing" && d.Phase != "queued" {
			return nil, Conflict("请先暂停并核对当前交付周期，不重复启动")
		}
		d.Phase = "queued"
		d.RoundID = r.CreatedID
		d.Version++
	} else {
		var cycle uint64
		if err = s.db.QueryRow("SELECT coalesce(max(cycle),0) FROM deliveries WHERE task_id=?", task.ID).Scan(&cycle); err != nil {
			return nil, err
		}
		d = Delivery{ID: id(), ProjectID: task.ProjectID, TaskID: task.ID, IntentID: r.CreatedID, RoundID: r.CreatedID, Cycle: cycle + 1, Version: 1, Phase: "queued", Policy: p, RepairsLimit: p.MaxRepairs, ActiveLimitSeconds: p.ActiveSeconds, StartedAt: time.Now().UTC()}
	}
	r.DeliveryID = d.ID
	public, err := s.deliverySummary(d, nil, nil)
	if err != nil {
		return nil, err
	}
	next.Deliveries[task.ID] = public
	body, _ := json.Marshal(d)
	return func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO deliveries(id,task_id,cycle,active,version,body) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET active=excluded.active,version=excluded.version,body=excluded.body", d.ID, d.TaskID, d.Cycle, 1, d.Version, body)
		return e
	}, nil
}
func (s *Store) AdvanceDelivery(key string, expectedVersion uint64, next Delivery, step *DeliveryStep) (Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.deliveryLocked(key)
	if err != nil {
		return Delivery{}, err
	}
	if old.Version != expectedVersion {
		return Delivery{}, Conflict("交付阶段已变化")
	}
	if next.ID != old.ID || next.TaskID != old.TaskID || next.ProjectID != old.ProjectID || next.IntentID != old.IntentID || next.Cycle != old.Cycle || deliveryFingerprint(next.Policy) != deliveryFingerprint(old.Policy) || (old.CommonDir != "" && next.CommonDir != old.CommonDir) {
		return Delivery{}, Conflict("交付身份或配置快照不可改写")
	}
	if !DeliveryActive(next.Phase) && next.Phase != "succeeded" && next.Phase != "cancelled" {
		return Delivery{}, Invalid("交付阶段无效")
	}
	if !DeliveryActive(old.Phase) {
		return Delivery{}, Conflict("交付周期已结束")
	}
	next.Version = old.Version + 1
	writeStep, prepared, err := s.prepareDeliveryStep(key, step)
	if err != nil {
		return Delivery{}, err
	}
	summary, err := s.deliverySummary(next, prepared, nil)
	if err != nil {
		return Delivery{}, err
	}
	state := clone(s.state)
	state.Deliveries[next.TaskID] = summary
	err = s.persistWith(state, "", "", Receipt{}, func(tx *sql.Tx) error {
		if writeStep != nil {
			if e := writeStep(tx); e != nil {
				return e
			}
		}
		return updateDeliveryRow(tx, next, old.Version)
	})
	if err != nil {
		return Delivery{}, err
	}
	return next, nil
}
func updateDeliveryRow(tx *sql.Tx, d Delivery, expected uint64) error {
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	active := 0
	if DeliveryActive(d.Phase) {
		active = 1
	}
	r, err := tx.Exec("UPDATE deliveries SET active=?,version=?,body=? WHERE id=? AND version=?", active, d.Version, body, d.ID, expected)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return Conflict("交付记录被其他实例修改")
	}
	return nil
}
