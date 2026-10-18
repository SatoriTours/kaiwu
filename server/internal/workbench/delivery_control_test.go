package workbench

import (
	"strings"
	"testing"
	"time"
)

// awaitingDelivery drives a freshly started cycle to the exact push-completed
// state the acceptance contract binds on. Token/tree/base/round are the same
// fields the manager fills between stages.
func awaitingDelivery(t *testing.T, s *Store, d Delivery, sha, tree, roundID, token string) Delivery {
	t.Helper()
	next := d
	next.Phase = "task_ci"
	next.CandidateSHA = sha
	next.CandidateTree = tree
	next.RoundID = roundID
	next.BaseMainSHA = "base-sha"
	next.LocalMainSHA = "base-sha"
	next.CIDeadline = time.Now().UTC().Add(30 * time.Minute)
	out, e := s.AdvanceDelivery(d.ID, d.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}
	next = out
	next.Phase = "awaiting_acceptance"
	next.AcceptanceToken = token
	out, e = s.AdvanceDelivery(d.ID, out.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestDeliveryAcceptanceStoreContract(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d, e := s.Delivery(r.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}

	// Wrong phase: nothing may consume an acceptance before the gate passed.
	if _, e := s.AcceptDelivery("accept-early", d.ID, d.AcceptanceToken, d.Version); e == nil {
		t.Fatal("acceptance accepted outside awaiting_acceptance")
	} else if !strings.Contains(e.Error(), "验收") {
		t.Fatalf("unexpected error: %v", e)
	}

	awaited := awaitingDelivery(t, s, d, "candidate-sha", "candidate-tree", r.CreatedID, "accept-token")

	if _, e := s.AcceptDelivery("accept-wrong", awaited.ID, "other-token", awaited.Version); e == nil {
		t.Fatal("wrong token consumed the acceptance")
	}
	if w, _ := s.Delivery(d.ID); w.Version != awaited.Version || w.Phase != "awaiting_acceptance" {
		t.Fatalf("failed acceptance still mutated state: %+v", w)
	}

	rec, e := s.AcceptDelivery("accept-ok", awaited.ID, "accept-token", awaited.Version)
	if e != nil {
		t.Fatal(e)
	}
	if rec.DeliveryID != d.ID || rec.CreatedID != "candidate-sha" || rec.Type != "delivery_accept" {
		t.Fatalf("receipt does not bind sha/cycle: %+v", rec)
	}
	w, e := s.Delivery(d.ID)
	if e != nil || w.Phase != "integrating" || w.AcceptedSHA != "candidate-sha" {
		t.Fatalf("acceptance not persisted: %+v %v", w, e)
	}

	rec2, e := s.AcceptDelivery("accept-ok", awaited.ID, "accept-token", awaited.Version)
	if e != nil || !rec2.Replay {
		t.Fatalf("same operation must replay: %+v %v", rec2, e)
	}
	if _, e := s.AcceptDelivery("accept-again", awaited.ID, "accept-token", w.Version); e == nil {
		t.Fatal("second acceptance of one token accepted")
	}
	// Stale version is always rejected before any state write.
	if _, e := s.AcceptDelivery("accept-stale", awaited.ID, "accept-token", awaited.Version+10); e == nil {
		t.Fatal("stale version accepted")
	}
}

func TestDeliveryControlStoreContract(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d0, e := s.Delivery(r.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	x0, _ := s.Delivery(d0.ID)
	next := x0
	next.Phase = "local_checking"
	next.RoundID = r.CreatedID
	d, e := s.AdvanceDelivery(d0.ID, d0.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}

	// Unknown action and non-empty token are refused outright.
	if _, e := s.ControlDelivery("ctl-bad", d.ID, "restart", "", d.Version); e == nil || !strings.Contains(e.Error(), "action") && !strings.Contains(e.Error(), "控制") {
		t.Fatalf("unknown action accepted: %v", e)
	}
	if _, e := s.ControlDelivery("ctl-token", d.ID, "pause", "token", d.Version); e == nil {
		t.Fatal("control actions must not mint token permissions")
	}

	_, e = s.ControlDelivery("ctl-pause", d.ID, "pause", "", d.Version)
	if e != nil {
		t.Fatal(e)
	}
	paused, _ := s.Delivery(d.ID)
	if e != nil || paused.Phase != "paused" {
		t.Fatalf("pause not persisted: %+v %v", paused, e)
	}
	rec2, e := s.ControlDelivery("ctl-pause", d.ID, "pause", "", d.Version)
	if e != nil || !rec2.Replay {
		t.Fatalf("pause must replay: %+v %v", rec2, e)
	}

	resumedRec, e := s.ControlDelivery("ctl-resume", d.ID, "resume", "", paused.Version)
	if e != nil {
		t.Fatal(e)
	}
	resumed, e := s.Delivery(d.ID)
	if e != nil || resumed.Phase != "local_checking" {
		t.Fatalf("resume not persisted: %+v %v", resumed, e)
	}
	if !resumed.CIDeadline.IsZero() {
		t.Fatalf("resume kept a CI deadline: %v", resumed.CIDeadline)
	}
	_ = resumedRec

	extendRec, e := s.ControlDelivery("ctl-extend", d.ID, "extend_budget", "", resumed.Version)
	if e != nil {
		t.Fatal(e)
	}
	_ = extendRec
	big, e := s.Delivery(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if big.RepairsLimit != d.RepairsLimit+3 || big.ActiveLimitSeconds != d.ActiveLimitSeconds+3600 {
		t.Fatalf("allowance not appended: %+v", big)
	}
	if big.RepairsUsed != 0 {
		t.Fatal("extend cleared the historical count")
	}
	p, e := s.DeliveryPolicy(project)
	if e != nil || p.MaxRepairs != 3 || p.ActiveSeconds != 3600 {
		t.Fatalf("policy snapshot altered: %+v %v", p, e)
	}
	audit := false
	for _, step := range s.Snapshot().Deliveries[task].Steps {
		if step.Kind == "extend_budget" {
			audit = true
		}
	}
	if !audit {
		t.Fatal("extend_budget missing its audit step")
	}
	if again, agErr := s.ControlDelivery("ctl-extend", d.ID, "extend_budget", "", d.Version); agErr != nil || !again.Replay {
		t.Fatalf("extend must replay by operation: %+v %v", again, agErr)
	}
	if grew, _ := s.Delivery(d.ID); grew.RepairsLimit != big.RepairsLimit {
		t.Fatal("replayed extend appended the allowance twice")
	}

	cancelRec, e := s.ControlDelivery("ctl-cancel", d.ID, "cancel", "", big.Version)
	if e != nil {
		t.Fatal(e)
	}
	_ = cancelRec
	gone, e := s.Delivery(d.ID)
	if e != nil || gone.Phase != "cancelled" || gone.FinishedAt.IsZero() {
		t.Fatalf("cancel not persisted: %+v %v", gone, e)
	}
	if active, e := s.ActiveDeliveries(); e != nil || len(active) != 0 {
		t.Fatalf("cancelled cycle still active: %+v %v", active, e)
	}
	// Blocked cycles may still be cancelled; paused→resume already covered.
	if _, e := s.ControlDelivery("ctl-cancel2", d.ID, "cancel", "", big.Version+50); e == nil {
		t.Fatal("wrong-version control accepted")
	}
}

func TestLatestDeliveryRepair(t *testing.T) {
	s, project, task, conversation, _ := repairStoreFixture(t)
	enableDelivery(t, s, project)
	if _, e := s.LatestDeliveryRepair("none"); e == nil {
		t.Fatal("missing repair returned no error")
	}
	start := startDelivery(t, s, project, task)
	finishRepairRound(t, s, conversation)
	d, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	d = blockDeliveryWithCandidate(t, s, d, "c1")
	claim1, _, e := claimOne(t, s, d, "c1", "task_ci", "fp-a", "第一次失败")
	if e != nil {
		t.Fatal(e)
	}
	finishRepairRound(t, s, conversation)
	d2, _ := s.Delivery(d.ID)
	d2 = blockDeliveryWithCandidate(t, s, d2, "c2")
	if _, ok, e := claimOne(t, s, d2, "c2", "task_ci", "fp-b", "第二次失败"); e != nil || !ok {
		t.Fatalf("second claim: %v ok=%v", e, ok)
	}
	last, e := s.LatestDeliveryRepair(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if last.Number != 2 || last.MessageID == claim1.MessageID || last.Phase != "claimed" {
		t.Fatalf("latest repair wrong: %+v", last)
	}
}
