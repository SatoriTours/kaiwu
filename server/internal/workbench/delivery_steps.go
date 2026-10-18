package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *Store) deliveryStepsLocked(key string, after uint64, limit int) (DeliveryStepPage, error) {
	if limit < 1 || limit > 100 {
		return DeliveryStepPage{}, Invalid("步骤分页大小需在 1–100")
	}
	rows, err := s.db.Query("SELECT body FROM delivery_steps WHERE delivery_id=? AND sequence>? ORDER BY sequence LIMIT ?", key, after, limit+1)
	if err != nil {
		return DeliveryStepPage{}, err
	}
	defer rows.Close()
	page := DeliveryStepPage{Items: []DeliveryStep{}}
	for rows.Next() {
		var body []byte
		var step DeliveryStep
		if err = rows.Scan(&body); err != nil {
			return page, err
		}
		if err = json.Unmarshal(body, &step); err != nil {
			return page, err
		}
		page.Items = append(page.Items, step)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = page.Items[limit-1].Sequence
	}
	return page, nil
}
func (s *Store) DeliverySteps(key string, after uint64, limit int) (DeliveryStepPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.deliveryLocked(key); err != nil {
		return DeliveryStepPage{}, err
	}
	return s.deliveryStepsLocked(key, after, limit)
}
func (s *Store) prepareDeliveryStep(key string, input *DeliveryStep) (func(*sql.Tx) error, *DeliveryStep, error) {
	if input == nil {
		return nil, nil, nil
	}
	step := *input
	if step.DeliveryID != key || !validText(step.ID, 100) || !validText(step.Kind, 100) || !validText(step.InputFingerprint, 256) || len(step.Summary) > 256<<10 || strings.ContainsRune(step.ID, 0) {
		return nil, nil, Invalid("交付步骤或摘要无效")
	}
	switch step.Phase {
	case "intent", "running", "observed", "uncertain":
	default:
		return nil, nil, Invalid("步骤阶段无效")
	}
	var oldBody []byte
	err := s.db.QueryRow("SELECT body FROM delivery_steps WHERE id=?", step.ID).Scan(&oldBody)
	if err == nil {
		var old DeliveryStep
		if err = json.Unmarshal(oldBody, &old); err != nil {
			return nil, nil, err
		}
		if old.DeliveryID != key || old.InputFingerprint != step.InputFingerprint || old.Kind != step.Kind {
			return nil, nil, Conflict("步骤标识已用于不同输入")
		}
		step.Sequence = old.Sequence
		if old.Phase == "observed" && deliveryFingerprint(old) != deliveryFingerprint(step) {
			return nil, nil, Conflict("不能改写已核实的步骤证据")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if err = s.db.QueryRow("SELECT coalesce(max(sequence),0)+1 FROM delivery_steps WHERE delivery_id=?", key).Scan(&step.Sequence); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}
	body, err := json.Marshal(step)
	if err != nil {
		return nil, nil, err
	}
	return func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO delivery_steps(id,delivery_id,sequence,fingerprint,body) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", step.ID, key, step.Sequence, step.InputFingerprint, body)
		return e
	}, &step, nil
}

func (s *Store) SaveDeliveryCI(key string, expectedVersion uint64, observations []DeliveryCI) (Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.deliveryLocked(key)
	if err != nil {
		return Delivery{}, err
	}
	if d.Version != expectedVersion {
		return Delivery{}, Conflict("CI 观察对应的交付阶段已变化")
	}
	if len(observations) > 64 {
		return Delivery{}, Invalid("CI 观察数量超限")
	}
	for _, o := range observations {
		want := d.CandidateSHA
		branch := "kaiwu/task-" + d.TaskID
		set := d.Policy.TaskWorkflows
		if o.Stage == "main" {
			want = d.LocalMainSHA
			branch = d.Policy.Repository.MainBranch
			set = d.Policy.MainWorkflows
		} else if o.Stage != "task" {
			return Delivery{}, Invalid("CI 阶段无效")
		}
		allowed := false
		for _, w := range set {
			if w.ID == o.WorkflowID {
				allowed = true
			}
		}
		prefix := "https://github.com/" + d.Policy.Repository.Owner + "/" + d.Policy.Repository.Name + "/actions/runs/"
		if o.DeliveryID != key || o.RepositoryID != d.Policy.Repository.ID || want == "" || o.SHA != want || o.Branch != branch || o.Event != "push" || !allowed || o.RunID <= 0 || o.Attempt < 1 || !strings.HasPrefix(o.URL, prefix) || len(o.URL) > 2048 || len(o.Jobs) > 256 {
			return Delivery{}, Conflict("CI 观察不属于当前已授权候选")
		}
		for name, result := range o.Jobs {
			if len(name) > 300 || len(result) > 100 {
				return Delivery{}, Invalid("CI job 摘要超限")
			}
		}
	}
	d.Version++
	summary, err := s.deliverySummary(d, nil, observations)
	if err != nil {
		return Delivery{}, err
	}
	state := clone(s.state)
	state.Deliveries[d.TaskID] = summary
	err = s.persistWith(state, "", "", Receipt{}, func(tx *sql.Tx) error {
		for _, o := range observations {
			b, e := json.Marshal(o)
			if e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT INTO delivery_ci(delivery_id,stage,workflow_id,run_id,attempt,body) VALUES(?,?,?,?,?,?) ON CONFLICT(delivery_id,stage,workflow_id,run_id,attempt) DO UPDATE SET body=excluded.body", key, o.Stage, o.WorkflowID, o.RunID, o.Attempt, b); e != nil {
				return e
			}
		}
		return updateDeliveryRow(tx, d, expectedVersion)
	})
	return d, err
}
