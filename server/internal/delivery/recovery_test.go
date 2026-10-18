package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// waitStep waits until a step of one kind reaches a phase, returning it.
func waitStep(t *testing.T, m *machine, kind, phase string) workbench.DeliveryStep {
	t.Helper()
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		m.clock.advance(time.Millisecond)
		page, e := m.store.DeliverySteps(m.deliveryID, 0, 100)
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range page.Items {
			if s.Kind == kind && s.Phase == phase {
				return s
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no running %s step appeared", kind)
	return workbench.DeliveryStep{}
}

// TestDeliveryCrashAfterMainPushBeforeReceipt drives the machine to each
// interruption window around the irreversible main push and proves recovery
// resolves it from records plus a read-only observation only: a landed push is
// not repeated, an unlanded intent is retried, an external move needs a human,
// the CI window keeps its original deadline and the queue slot survives.
func TestDeliveryCrashAfterMainPushBeforeReceipt(t *testing.T) {
	ctx := context.Background()

	t.Run("landed push is backfilled without a second push", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		setCrashPoint("after-main-push")
		t.Cleanup(clearCrashPoint)
		if _, e := m.manager.Accept(ctx, "accept-crash", d.ID, d.AcceptanceToken, d.Version); e != nil {
			t.Fatal(e)
		}
		intent := waitStep(t, m, "push_main", "running")
		pushes := m.remote.pushCount()
		mains := countMainPushes(m.remote.pushedRefs())

		clearCrashPoint()
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		w := m.mustDelivery()
		if w.Phase != "main_ci" {
			t.Fatalf("recovery phase %q", w.Phase)
		}
		if m.remote.pushCount() != pushes || countMainPushes(m.remote.pushedRefs()) != mains {
			t.Fatal("recovery pushed the already landed main again")
		}
		// The original CI window keeps its deadline instead of gaining budget.
		want := intent.StartedAt.Add(time.Duration(w.Policy.CISeconds) * time.Second)
		if !w.CIDeadline.Equal(want) {
			t.Fatalf("recovered deadline %v, want %v", w.CIDeadline, want)
		}
		// The logical slot is retained and the old CI attempt still closes the
		// cycle once it passes.
		if ok, e := m.store.AcquireDeliveryTurn(w.ID); e != nil || !ok {
			t.Fatalf("queue slot lost in recovery: %v %v", ok, e)
		}
		m.waitPhase(t, "succeeded")
		if countMainPushes(m.remote.pushedRefs()) != mains {
			t.Fatal("recovery caused an extra main push")
		}
	})

	t.Run("unlanded intent is retried exactly once", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		setCrashPoint("after-main-intent")
		t.Cleanup(clearCrashPoint)
		if _, e := m.manager.Accept(ctx, "accept-intent", d.ID, d.AcceptanceToken, d.Version); e != nil {
			t.Fatal(e)
		}
		waitStep(t, m, "push_main", "running")
		mains := countMainPushes(m.remote.pushedRefs())

		clearCrashPoint()
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		// The stage re-runs: exactly one main push, and the cycle completes.
		m.waitPhase(t, "succeeded")
		if got := countMainPushes(m.remote.pushedRefs()); got != mains+1 {
			t.Fatalf("main pushes %d, want exactly one retry", got)
		}
	})

	t.Run("receipt already written needs no recovery work", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		setCrashPoint("after-main-receipt")
		t.Cleanup(clearCrashPoint)
		if _, e := m.manager.Accept(ctx, "accept-receipt", d.ID, d.AcceptanceToken, d.Version); e != nil {
			t.Fatal(e)
		}
		waitStep(t, m, "push_main", "observed")
		pushes := m.remote.pushCount()
		clearCrashPoint()
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		m.waitPhase(t, "succeeded")
		if m.remote.pushCount() != pushes {
			t.Fatal("recovery repeated a completed push")
		}
	})

	t.Run("external move parks the cycle instead of guessing", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		setCrashPoint("after-main-push")
		t.Cleanup(clearCrashPoint)
		if _, e := m.manager.Accept(ctx, "accept-foreign", d.ID, d.AcceptanceToken, d.Version); e != nil {
			t.Fatal(e)
		}
		waitStep(t, m, "push_main", "running")
		// Someone else moved the authorized main ref.
		m.remote.inject(strings.Repeat("e", 40), "refs/heads/main")
		pushes := m.remote.pushCount()

		clearCrashPoint()
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		w := m.mustDelivery()
		if w.Phase != "needs_check" || w.BlockCode != "recovery_uncertain" {
			t.Fatalf("foreign move not parked: %+v", w)
		}
		if m.remote.pushCount() != pushes {
			t.Fatal("recovery wrote over a foreign main")
		}
	})
}

// TestDeliveryRecoveryWindowsKeepBudgetAndAuthorization proves recovery never
// resets budgets or deadlines, never re-sends an unknown model dispatch, only
// backfills uniquely provable commits and blocks writes for revoked
// authorizations.
func TestDeliveryRecoveryWindowsKeepBudgetAndAuthorization(t *testing.T) {
	ctx := context.Background()

	t.Run("an interrupted repair keeps its budget and never re-dispatches", func(t *testing.T) {
		m := newMachine(t, true)
		m.ci.setOpen(false, true, false, false)
		m.ci.setFailedJob(true, "Error: undefined variable app broken")
		m.finishRound(happyReport())
		job := driveOneRepair(t, m)
		rav := m.waitPhase(t, "repairing")
		before := *m.mustDelivery()
		dispatches := m.agent.repairsCount()
		if job.MessageID == "" || dispatches != 1 {
			t.Fatalf("fixture did not dispatch one repair: %d", dispatches)
		}
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		after := *m.mustDelivery()
		if after.Phase != rav.Phase || after.RepairsUsed != before.RepairsUsed ||
			!after.CIDeadline.Equal(before.CIDeadline) || !after.ActiveSince.Equal(before.ActiveSince) {
			t.Fatalf("recovery mutated repair budget/state: %+v -> %+v", before, after)
		}
		if m.agent.repairsCount() != dispatches {
			t.Fatal("recovery re-sent an unknown repair dispatch")
		}
	})

	t.Run("only a uniquely provable commit is backfilled", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		tree := m.tree
		before := headOf(t, tree)
		writeFile(m.t, tree+"/committed.txt", "committed\n")
		after := commitAllIn(t, tree)
		if before == after {
			t.Fatal("fixture commit did not move HEAD")
		}
		provable := workbench.DeliveryStep{
			ID: "recover-commit-1", DeliveryID: d.ID, Kind: "commit", InputFingerprint: after,
			Phase: "running", BeforeSHA: before, AfterSHA: after, StartedAt: time.Now().UTC(),
		}
		if _, e := m.store.AdvanceDelivery(d.ID, d.Version, d, &provable); e != nil {
			t.Fatal(e)
		}
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		got := stepByID(t, m, "recover-commit-1")
		if got.Phase != "observed" || got.AfterSHA != after {
			t.Fatalf("provable commit not backfilled: %+v", got)
		}

		ambiguous := workbench.DeliveryStep{
			ID: "recover-commit-2", DeliveryID: d.ID, Kind: "commit", InputFingerprint: after,
			Phase: "running", BeforeSHA: strings.Repeat("c", 40), AfterSHA: after, StartedAt: time.Now().UTC(),
		}
		cur := *m.mustDelivery()
		if _, e := m.store.AdvanceDelivery(d.ID, cur.Version, cur, &ambiguous); e != nil {
			t.Fatal(e)
		}
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		got = stepByID(t, m, "recover-commit-2")
		if got.Phase != "uncertain" {
			t.Fatalf("ambiguous commit was backfilled: %+v", got)
		}
	})

	t.Run("revoked authorization blocks instead of resuming writes", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "awaiting_acceptance")
		p := policySnapshot(t, m.store)
		// Publishing an unenabled policy revokes the authorization in store.
		p.Enabled = false
		if _, e := m.store.SaveDeliveryPolicy("revoke-recovery", p.Version, p); e != nil {
			t.Fatal(e)
		}
		pushes := m.remote.pushCount()
		if e := m.manager.Recover(ctx); e != nil {
			t.Fatal(e)
		}
		w := m.mustDelivery()
		if w.Phase != "blocked" || (w.BlockCode != "authorization_disabled" && w.BlockCode != "authorization_revoked") {
			t.Fatalf("revoked cycle resumed: %+v", w)
		}
		if m.remote.pushCount() != pushes {
			t.Fatal("revoked recovery wrote to the remote")
		}
		_ = d
	})
}

func stepByID(t *testing.T, m *machine, id string) workbench.DeliveryStep {
	t.Helper()
	page, e := m.store.DeliverySteps(m.deliveryID, 0, 100)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range page.Items {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("step %s missing", id)
	return workbench.DeliveryStep{}
}
