package workbench

import (
	"path/filepath"
	"strings"
	"testing"
)

// repairStoreFixture mirrors deliveryFixture but also returns the sqlite path
// so the shared-repair-budget test can prove the ledger survives a restart.
func repairStoreFixture(t *testing.T) (s *Store, project, task, conversation, dbPath string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "state.db")
	s, e := Open(dbPath, Profile{Provider: "fixture", Model: "baseline"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	command := func(c Command) Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project = command(Command{OperationID: "create-project", Type: "add_project", Name: "fixture", Path: t.TempDir()}).CreatedID
	task = command(Command{OperationID: "create-task", Type: "add_task", ProjectID: project, TopicID: project + "_other", Title: "feature", Request: "requirement"}).CreatedID
	conversation = s.Snapshot().Tasks[task].ConversationID
	return
}

// finishRound simulates the repair dispatch settling so the same conversation
// can claim the next repair; a real dispatch would come from the agent port.
func finishRepairRound(t *testing.T, s *Store, conversation string) {
	t.Helper()
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conversation]
		c.RunState = "idle"
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" {
				c.Messages[i].Delivery = "accepted"
			}
		}
		for i := range c.Rounds {
			if c.Rounds[i].Phase == "running" {
				c.Rounds[i].Phase = "finished"
			}
		}
		st.Conversations[conversation] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func blockDeliveryWithCandidate(t *testing.T, s *Store, d Delivery, sha string) Delivery {
	t.Helper()
	next := d
	next.Phase = "blocked"
	next.CandidateSHA = sha
	out, e := s.AdvanceDelivery(d.ID, d.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestDeliveryRepairSharedBudget(t *testing.T) {
	s, project, task, conversation, dbPath := repairStoreFixture(t)

	enableDelivery(t, s, project)
	start := startDelivery(t, s, project, task)
	finishRepairRound(t, s, conversation)
	d, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	d = blockDeliveryWithCandidate(t, s, d, "c1")
	if p, e := s.DeliveryPolicy(project); e != nil || p.MaxRepairs != 3 {
		t.Fatalf("expected default shared budget 3, got %+v %v", p, e)
	}

	claim1, ok1, e := claimOne(t, s, d, "c1", "task_ci", "fp-a", "任务 CI 失败摘要")
	if e != nil || !ok1 {
		t.Fatalf("first repair must prepare+claim: %v ok=%v", e, ok1)
	}
	if claim1.Number != 1 || claim1.Stage != "task_ci" || claim1.FailedSHA != "c1" || claim1.FailureFingerprint != "fp-a" {
		t.Fatalf("repair record mismatch: %+v", claim1)
	}
	if claim1.Profile.Model != "baseline" {
		t.Fatalf("repair must record the actual profile, got %+v", claim1.Profile)
	}
	if !strings.Contains(claim1.Prompt, "任务 CI 失败摘要") || !strings.Contains(claim1.Prompt, "不要自动提交、合并、推送") {
		t.Fatalf("repair prompt missing summary or release ban: %q", claim1.Prompt)
	}

	conversation = s.Snapshot().Tasks[task].ConversationID
	c := s.Snapshot().Conversations[conversation]
	var system []Message
	for _, m := range c.Messages {
		if m.Role == "system" {
			system = append(system, m)
		}
	}
	if len(system) != 1 || system[0].Kind != "automatic_repair" || system[0].Text != claim1.Prompt || system[0].Delivery != "pending" {
		t.Fatalf("expected exactly one system repair message, got %+v", system)
	}
	round := c.Rounds[len(c.Rounds)-1]
	if round.ID != claim1.MessageID || round.Origin != "system_repair" || round.DeliveryID != d.ID || round.Profile.Model != "baseline" {
		t.Fatalf("system round markers missing: %+v", round)
	}
	if c.RunState != "starting" {
		t.Fatalf("claim must admit the dispatch, got %q", c.RunState)
	}

	d2, e := s.Delivery(d.ID)
	if e != nil || d2.RepairsUsed != 1 || d2.Version <= d.Version {
		t.Fatalf("claim must consume shared budget once, got %+v %v", d2, e)
	}

	// Same job re-claim must not re-charge or re-queue (unknown request outcome).
	again, ok2, e := s.ClaimDeliveryRepair(claim1.ID)
	if e != nil || ok2 || again.Number != 1 || again.MessageID != claim1.MessageID {
		t.Fatalf("re-claim must be a no-op: %v ok=%v %+v", e, ok2, again)
	}
	if got := s.Snapshot().Conversations[conversation]; len(got.Messages) != len(c.Messages) {
		t.Fatalf("re-claim duplicated the system message: %+v", got)
	}
	if d2b, e := s.Delivery(d.ID); e != nil || d2b.RepairsUsed != 1 {
		t.Fatalf("re-claim re-charged the budget: %+v", d2b)
	}

	// Identical fingerprint with an unchanged candidate stops instead of looping.
	finishRepairRound(t, s, conversation)
	d3 := blockDeliveryWithCandidate(t, s, d2, "c1")
	if _, e := s.PrepareDeliveryRepair(d.ID, d3.Version, "c1", "task_ci", "fp-a", "同样的失败仍在原地"); e == nil {
		t.Fatal("unchanged same-fingerprint failure must stop")
	} else if !strings.Contains(e.Error(), "无新变更") && !strings.Contains(e.Error(), "停止自动修复") {
		t.Fatalf("unexpected stop reason: %v", e)
	}

	// After the candidate changes, the same fingerprint is repairable again.
	d4 := blockDeliveryWithCandidate(t, s, d3, "c2")
	claim2, ok4, e := claimOne(t, s, d4, "c2", "task_ci", "fp-a", "候选已更新仍失败")
	if e != nil || !ok4 || claim2.Number != 2 {
		t.Fatalf("changed candidate must allow repair: %v ok=%v", e, ok4)
	}
	finishRepairRound(t, s, conversation)
	d5, e := s.Delivery(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	claim3, ok5, e := claimOne(t, s, d5, "c2", "task_ci", "fp-b", "又一次失败")
	if e != nil || !ok5 || claim3.Number != 3 {
		t.Fatalf("second fingerprint repair must use the shared ledger: %v ok=%v", e, ok5)
	}
	d6, err6 := s.Delivery(d.ID)
	if err6 != nil || d6.RepairsUsed != 3 {
		t.Fatalf("shared budget must be at limit here: %+v %v", d6, err6)
	}

	// Budget exhausted: refuses regardless of stage or fingerprint.
	if _, e := s.PrepareDeliveryRepair(d.ID, d6.Version, "c2", "main_ci", "fp-c", "主 CI 失败"); e == nil {
		t.Fatal("exhausted budget must refuse new repairs")
	} else if !strings.Contains(e.Error(), "修复额度") {
		t.Fatalf("unexpected budget refusal: %v", e)
	}
	// A stale expectedVersion is always rejected before any state is written.
	if _, e := s.PrepareDeliveryRepair(d.ID, d6.Version+1, "c2", "main_ci", "fp-c", "版本不匹配拒绝"); e == nil || !strings.Contains(e.Error(), "交付阶段已变化") {
		t.Fatalf("stale expectedVersion must conflict: %v", e)
	}

	_e := s.Close()
	if _e != nil {
		t.Fatal(_e)
	}
	s2, e := Open(dbPath, Profile{Provider: "fixture", Model: "baseline"})
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	r2, e := s2.Delivery(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s2.PrepareDeliveryRepair(r2.ID, r2.Version, "c2", "task_ci", "fp-restart", "重启后仍未修复"); e == nil || !strings.Contains(e.Error(), "修复额度") {
		t.Fatalf("budget must survive restart: %v", e)
	}
}

// claimOne performs prepare+claim for one repair round.
func claimOne(t *testing.T, s *Store, d Delivery, failedSHA, stage, fingerprint, summary string) (DeliveryRepair, bool, error) {
	t.Helper()
	r, e := s.PrepareDeliveryRepair(d.ID, d.Version, failedSHA, stage, fingerprint, summary)
	if e != nil {
		return DeliveryRepair{}, false, e
	}
	return s.ClaimDeliveryRepair(r.ID)
}
