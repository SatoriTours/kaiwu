package delivery

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDeliveryMainFailureQuarantinesQueue proves the integration lane stays
// quarantined after a main-branch failure: only the original task keeps
// repairing with its own session, the next task waits in the persisted queue,
// the failed main push is never reset away and both sessions survive.
func TestDeliveryMainFailureQuarantinesQueue(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setMainFailed(true)
	m.ci.setFailedJob(true, "Error: undefined variable deploy target")

	// Task A: task gate passes, integration slot acquired, candidate waits for
	// the human acceptance while the main gate is still broken.
	m.finishRound(happyReport())
	d := m.waitPhase(t, "awaiting_acceptance")

	// Task B shares the same clone and the same remote main lane.
	_, deliveryB, roundB := m.addSecondTask(t)
	m.finishRoundIn("c2", roundB, happyReport())
	m.waitPhaseNamed(t, deliveryB, "preparing_integration")
	waitLaneRefused(t, m, deliveryB)

	// A accepts: the candidate integrates locally, the main push happens and
	// the main CI failure routes a repair to A's own session only.
	if _, e := m.manager.Accept(context.Background(), "accept-A", d.ID, d.AcceptanceToken, d.Version); e != nil {
		t.Fatal(e)
	}
	rav := m.waitPhase(t, "repairing")
	if rav.BlockCode != "" {
		t.Fatalf("main failure blocked instead of repairing: %+v", rav)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("repair dispatches %d, want 1", got)
	}
	repaired := false
	for _, msg := range m.store.Snapshot().Conversations["c"].Messages {
		if msg.Kind == "automatic_repair" {
			repaired = true
		}
	}
	if !repaired {
		t.Fatal("main failure did not route a repair to the original session")
	}
	for _, msg := range m.store.Snapshot().Conversations["c2"].Messages {
		if msg.Kind == "automatic_repair" {
			t.Fatal("main failure was charged to the waiting task")
		}
	}
	// The failed main SHA was really pushed and stays in the remote record.
	mainPushes := 0
	for _, p := range m.remote.pushedRefs() {
		if strings.HasSuffix(p, "refs/heads/main") {
			mainPushes++
		}
	}
	if mainPushes != 1 {
		t.Fatalf("main pushes %d, want 1 before the failure repair", mainPushes)
	}
	if b := m.waitPhaseNamed(t, deliveryB, "preparing_integration"); b.Phase != "preparing_integration" {
		t.Fatalf("waiting cycle left the queue: %s", b.Phase)
	}

	// A repairs inside its own worktree and must be revalidated by the human
	// because the source changed again.
	m.settleNextRepair("fix-main.txt")
	again := m.waitPhase(t, "awaiting_acceptance")
	if again.AcceptanceToken == d.AcceptanceToken {
		t.Fatal("revalidated cycle reused the stale acceptance token")
	}
	m.ci.setMainFailed(false)
	if _, e := m.manager.Accept(context.Background(), "accept-A2", again.ID, again.AcceptanceToken, again.Version); e != nil {
		t.Fatal(e)
	}
	fin := m.waitPhase(t, "succeeded")
	if fin.AcceptedSHA != again.CandidateSHA {
		t.Fatalf("second acceptance not bound to the repaired candidate: %+v", fin)
	}
	// The verified release hands the lane on: the waiting task resyncs onto
	// the new main before its own acceptance.
	m.waitPhaseNamed(t, deliveryB, "awaiting_acceptance")
	if ok, e := m.store.AcquireDeliveryTurn(deliveryB); e != nil || !ok {
		t.Fatalf("lane was not handed to the waiting task: %v %v", ok, e)
	}
	if mainPushes := countMainPushes(m.remote.pushedRefs()); mainPushes != 2 {
		t.Fatalf("main pushes %d, want the second verified integration", mainPushes)
	}
	snap := m.store.Snapshot()
	if len(snap.Conversations["c"].Messages) == 0 || len(snap.Conversations["c2"].Messages) == 0 {
		t.Fatal("a session was lost during the quarantine")
	}
}

// waitLaneRefused waits until the cycle is enqueued and the lane provably
// refuses to hand it the turn.
func waitLaneRefused(t *testing.T, m *machine, deliveryID string) {
	t.Helper()
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		m.clock.advance(time.Millisecond)
		ok, e := m.store.AcquireDeliveryTurn(deliveryID)
		if e == nil && !ok {
			return
		}
		if e != nil && strings.Contains(e.Error(), "不存在") {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the waiting cycle was not held back by the integration lane")
}

func countMainPushes(pushes []string) int {
	n := 0
	for _, p := range pushes {
		if strings.HasSuffix(p, "refs/heads/main") {
			n++
		}
	}
	return n
}
