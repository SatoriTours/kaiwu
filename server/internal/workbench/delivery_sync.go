package workbench

import (
	"encoding/json"
	"sort"
	"strconv"
	"unicode/utf8"
)

func deliveryExcerpt(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text, true
}
func (s *Store) deliverySummary(d Delivery, changed *DeliveryStep, ci []DeliveryCI) (DeliverySummary, error) {
	summary := DeliverySummary{ID: d.ID, ProjectID: d.ProjectID, TaskID: d.TaskID, Phase: d.Phase, Version: d.Version, Cycle: d.Cycle, CandidateSHA: d.CandidateSHA, LocalMainSHA: d.LocalMainSHA, RemoteMainSHA: d.RemoteMainSHA, RequireAcceptance: d.Policy.RequireAcceptance, AcceptanceToken: d.AcceptanceToken, AcceptedSHA: d.AcceptedSHA, RepairsUsed: d.RepairsUsed, RepairsLimit: d.RepairsLimit, BlockCode: d.BlockCode, BlockReason: d.BlockReason, WaitingCode: d.WaitingCode, WaitingReason: d.WaitingReason, HoldsTurn: d.HoldsTurn, Steps: []DeliveryStep{}, CI: []DeliveryCI{}}
	rows, err := s.db.Query("SELECT body FROM delivery_steps WHERE delivery_id=? ORDER BY sequence DESC LIMIT 20", d.ID)
	if err != nil {
		return summary, err
	}
	for rows.Next() {
		var body []byte
		var step DeliveryStep
		if err = rows.Scan(&body); err != nil {
			break
		}
		if err = json.Unmarshal(body, &step); err != nil {
			break
		}
		summary.Steps = append(summary.Steps, step)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return summary, err
	}
	if changed != nil {
		found := false
		for i := range summary.Steps {
			if summary.Steps[i].ID == changed.ID {
				summary.Steps[i] = *changed
				found = true
			}
		}
		if !found {
			summary.Steps = append(summary.Steps, *changed)
		}
	}
	sort.Slice(summary.Steps, func(i, j int) bool { return summary.Steps[i].Sequence < summary.Steps[j].Sequence })
	if len(summary.Steps) > 20 {
		summary.Steps = summary.Steps[len(summary.Steps)-20:]
	}
	for i := range summary.Steps {
		excerpt, cut := deliveryExcerpt(summary.Steps[i].Summary, 4096)
		summary.Steps[i].Summary = excerpt
		summary.Steps[i].Truncated = summary.Steps[i].Truncated || cut
		summary.Steps[i].BootID = ""
		summary.Steps[i].ProcessGroup = 0
	}
	// History stays in SQL. Only current SHA/required-workflow observations enter
	// the snapshot, with the newest attempt taking precedence within one run.
	latest := map[string]DeliveryCI{}
	add := func(o DeliveryCI) {
		expected := d.CandidateSHA
		if o.Stage == "main" {
			expected = d.LocalMainSHA
		}
		if o.SHA != expected || expected == "" {
			return
		}
		key := o.Stage + ":" + strconv.FormatInt(o.WorkflowID, 10)
		old, ok := latest[key]
		if !ok || o.RunID > old.RunID || (o.RunID == old.RunID && o.Attempt >= old.Attempt) {
			latest[key] = o
		}
	}
	rows, err = s.db.Query("SELECT body FROM delivery_ci WHERE delivery_id=?", d.ID)
	if err != nil {
		return summary, err
	}
	for rows.Next() {
		var b []byte
		var o DeliveryCI
		if err = rows.Scan(&b); err != nil {
			break
		}
		if err = json.Unmarshal(b, &o); err != nil {
			break
		}
		add(o)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return summary, err
	}
	for _, o := range ci {
		add(o)
	}
	keys := []string{}
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		summary.CI = append(summary.CI, latest[key])
	}
	return summary, nil
}
