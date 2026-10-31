package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestDeliveryDormantBeforeRecovery(t *testing.T) {
	m := newMachineWithStartup(t, true, nil, false)
	if err := m.manager.Tick(context.Background(), m.deliveryID); err == nil {
		t.Fatal("Tick was accepted before initial recovery; worker has no startup barrier")
	}
	if len(m.agent.sent()) != 0 || m.remote.pushCount() != 0 {
		t.Fatal("dormant manager started work")
	}
	if err := m.manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.waitPhase(t, "developing")
	if len(m.agent.sent()) != 1 {
		t.Fatal("initial recovery did not release exactly one dispatch")
	}
}

// This isolated stage fixture has no live background pass. It deliberately
// invokes real stage/reconciliation functions against the real store and Git.
func recoveryStageFixture(t *testing.T) (*machine, *Manager, workbench.Delivery) {
	t.Helper()
	m := newMachine(t, true)
	m.finishRound(happyReport())
	d := m.waitPhase(t, "awaiting_acceptance")
	if err := m.manager.Close(); err != nil {
		t.Fatal(err)
	}
	detached := &Manager{ctx: context.Background(), store: m.store, cfg: m.manager.cfg}
	return m, detached, d
}

func TestRecoveryExcludesConcurrentTick(t *testing.T) {
	m, _, d := recoveryStageFixture(t)
	d.Phase = "pushing_main"
	s := workbench.DeliveryStep{ID: "serialized-main", DeliveryID: d.ID, Kind: "push_main", Phase: "running", InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: "refs/heads/main", StartedAt: m.clock.Now()}
	var err error
	if _, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s); err != nil {
		t.Fatal(err)
	}
	m.remote.inject(d.CandidateSHA, s.Ref)
	cfg := m.manager.cfg
	entered, overlap, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first, overlapOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var active atomic.Int32
	transport := m.remote.transport(m.store, m.taskID)
	cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, ref string, lease gitops.ProcessLease) (string, error) {
		if args[0] == "ls-remote" {
			if active.Add(1) > 1 {
				overlapOnce.Do(func() { close(overlap) })
			}
			defer active.Add(-1)
			first.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return transport(ctx, dir, target, args, ref, lease)
	})})
	manager, err := New(context.Background(), m.store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	recoverDone := make(chan error, 1)
	go func() { recoverDone <- manager.Recover(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery did not reach controlled observation")
	}
	tickDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go func() { tickDone <- manager.Tick(ctx, d.ID) }()
	select {
	case <-overlap:
		t.Error("Tick observed the remote concurrently with recovery")
	case err := <-tickDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("queued Tick did not honor caller cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Tick did not return within caller deadline")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-recoverDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(3 * time.Second):
		t.Error("recovery did not finish")
	}
}

func TestPendingPushUncertaintyMatchesRecovery(t *testing.T) {
	for _, route := range []string{"live", "recovery"} {
		t.Run(route, func(t *testing.T) {
			m, manager, d := recoveryStageFixture(t)
			d.Phase = "pushing_main"
			s := workbench.DeliveryStep{ID: "pending-main", DeliveryID: d.ID, Kind: "push_main", Phase: "running", InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: "refs/heads/main", StartedAt: m.clock.Now()}
			var err error
			d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
			if err != nil {
				t.Fatal(err)
			}
			m.remote.inject(strings.Repeat("e", 40), s.Ref)
			pushes := m.remote.pushCount()
			if route == "live" {
				_, handled, cont := manager.resolvePendingIntent(d, s.Kind)
				if !handled || cont {
					t.Fatal("uncertain intent was allowed to repeat")
				}
			} else if err := manager.recoverOne(context.Background(), d); err != nil {
				t.Fatal(err)
			}
			got := m.mustDelivery()
			if got.Phase != "needs_check" || got.BlockCode != "recovery_uncertain" || stepByID(t, m, s.ID).Phase != "uncertain" {
				t.Fatalf("ambiguous intent not consistently parked: phase=%s code=%s step=%s", got.Phase, got.BlockCode, stepByID(t, m, s.ID).Phase)
			}
			if m.remote.pushCount() != pushes || m.agent.repairsCount() != 0 {
				t.Fatal("uncertain intent caused another effect")
			}
		})
	}
}

func TestRecoveryFindsPendingIntentBeyondFirstPage(t *testing.T) {
	for _, route := range []string{"live", "recovery"} {
		t.Run(route, func(t *testing.T) {
			m, manager, d := recoveryStageFixture(t)
			d.Phase = "pushing_main"
			for i := 0; i < 101; i++ {
				s := workbench.DeliveryStep{ID: fmt.Sprintf("history-%d", i), DeliveryID: d.ID, Kind: "note", Phase: "observed", InputFingerprint: "history"}
				var err error
				d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
				if err != nil {
					t.Fatal(err)
				}
			}
			s := workbench.DeliveryStep{ID: "late-push", DeliveryID: d.ID, Kind: "push_main", Phase: "running", InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: "refs/heads/main", StartedAt: m.clock.Now()}
			var err error
			d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
			if err != nil {
				t.Fatal(err)
			}
			m.remote.inject(strings.Repeat("e", 40), s.Ref)
			pushes := m.remote.pushCount()
			if route == "live" {
				_, handled, cont := manager.resolvePendingIntent(d, s.Kind)
				if !handled || cont {
					t.Error("late unknown intent was treated as absent")
				}
			} else if err := manager.recoverOne(context.Background(), d); err != nil {
				t.Fatal(err)
			}
			if got := m.mustDelivery(); got.Phase != "needs_check" || got.BlockCode != "recovery_uncertain" {
				t.Fatalf("late intent not parked: phase=%s code=%s", got.Phase, got.BlockCode)
			}
			if m.remote.pushCount() != pushes {
				t.Fatal("late intent caused another push")
			}
		})
	}
}

func TestRecoveryReconcilesMultiplePendingSteps(t *testing.T) {
	m, manager, d := recoveryStageFixture(t)
	d.Phase = "pushing_main"
	for _, kind := range []string{"push_task", "push_main"} {
		s := workbench.DeliveryStep{ID: "multiple-" + kind, DeliveryID: d.ID, Kind: kind, Phase: "running", InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: "refs/heads/main", StartedAt: m.clock.Now()}
		if kind == "push_task" {
			s.Ref = "refs/heads/kaiwu/task-" + d.TaskID
		}
		var err error
		d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
		if err != nil {
			t.Fatal(err)
		}
	}
	m.remote.inject(strings.Repeat("e", 40), "refs/heads/main")
	if err := manager.recoverOne(context.Background(), d); err != nil {
		t.Fatalf("recovery reused a stale CAS version: %v", err)
	}
	if got := m.mustDelivery(); got.Phase != "needs_check" {
		t.Fatalf("later uncertain intent not parked: %s", got.Phase)
	}
	if stepByID(t, m, "multiple-push_task").Phase != "observed" || stepByID(t, m, "multiple-push_main").Phase != "uncertain" {
		t.Fatal("not every pending step was reconciled")
	}
}

func TestRecoveryConflictDoesNotOpenStartupBarrier(t *testing.T) {
	m, _, d := recoveryStageFixture(t)
	d.Phase = "pushing_main"
	s := workbench.DeliveryStep{ID: "conflicting-main", DeliveryID: d.ID, Kind: "push_main", Phase: "running", InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: "refs/heads/main", StartedAt: m.clock.Now()}
	var err error
	d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &s)
	if err != nil {
		t.Fatal(err)
	}
	m.remote.inject(strings.Repeat("e", 40), s.Ref)
	cfg := m.manager.cfg
	var once sync.Once
	transport := m.remote.transport(m.store, m.taskID)
	cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, ref string, lease gitops.ProcessLease) (string, error) {
		once.Do(func() {
			current, e := m.store.Delivery(d.ID)
			if e != nil {
				t.Error(e)
				return
			}
			if _, e = m.store.AdvanceDelivery(current.ID, current.Version, current, nil); e != nil {
				t.Error(e)
			}
		})
		return transport(ctx, dir, target, args, ref, lease)
	})})
	manager, err := New(context.Background(), m.store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.Recover(context.Background()); err == nil {
		t.Error("incomplete conflicting reconciliation was reported as successful")
	}
	if err := manager.Tick(context.Background(), d.ID); err == nil {
		t.Error("failed recovery opened the startup barrier")
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal("fresh explicit reconciliation could not recover:", err)
	}
	if got := m.mustDelivery(); got.Phase != "needs_check" {
		t.Fatalf("fresh reconciliation did not park ambiguity: %s", got.Phase)
	}
}

func TestRecoveryGateCASFailureIsNotSuccess(t *testing.T) {
	m, manager, d := recoveryStageFixture(t)
	p := policySnapshot(t, m.store)
	p.Enabled = false
	if _, err := m.store.SaveDeliveryPolicy("revoke-before-conflict", p.Version, p); err != nil {
		t.Fatal(err)
	}
	current := m.mustDelivery()
	if _, err := m.store.AdvanceDelivery(current.ID, current.Version, *current, nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.recoverOne(context.Background(), d); err == nil {
		t.Fatal("failed gate-state CAS was reported as successful recovery")
	}
}

// The whole same-kind pending set must be resolved before a live stage may
// create another push or advance based on one observed result.
func TestPendingPushBatchMustBeFullyReconciled(t *testing.T) {
	cases := []struct{ name, first, second string }{
		{"unstarted-then-uncertain", "not_started", "uncertain"},
		{"observed-then-uncertain", "observed", "uncertain"},
		{"all-uncertain", "uncertain", "uncertain"},
		{"all-unstarted", "not_started", "not_started"},
		{"all-observed", "observed", "observed"},
		{"observed-then-unstarted", "observed", "not_started"},
		{"unstarted-then-observed", "not_started", "observed"},
	}
	for _, kind := range []string{"task", "main"} {
		for _, route := range []string{"live", "recovery"} {
			for _, tc := range cases {
				t.Run(kind+"/"+route+"/"+tc.name, func(t *testing.T) {
					m, manager, d := recoveryStageFixture(t)
					d.Phase = "pushing_" + kind
					ref := "refs/heads/kaiwu/task-" + d.TaskID
					if kind == "main" {
						gitIn(t, m.source, "merge", "--ff-only", d.CandidateSHA)
						d.LocalMainSHA = d.CandidateSHA
						ref = "refs/heads/main"
					}
					remote := d.CandidateSHA
					if tc.first == "not_started" && tc.second != "observed" {
						remote = m.baseSHA
					}
					m.remote.inject(remote, ref)
					expectedStates := []string{tc.first, tc.second}
					steps := make([]workbench.DeliveryStep, 2)
					for i, state := range expectedStates {
						input, before := d.CandidateSHA, remote
						if state == "uncertain" {
							input, before = strings.Repeat("c", 40), strings.Repeat("e", 40)
						}
						if state == "not_started" && input == remote {
							input = strings.Repeat("c", 40)
						}
						steps[i] = workbench.DeliveryStep{
							ID: fmt.Sprintf("batch-%d", i), DeliveryID: d.ID, Kind: "push_" + kind, Phase: "running",
							InputFingerprint: input, BeforeSHA: before, Ref: ref,
							StartedAt: m.clock.Now().Add(time.Duration(-3+2*i) * time.Second),
						}
						var err error
						d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &steps[i])
						if err != nil {
							t.Fatal(err)
						}
					}
					for i, s := range steps {
						control, err := manager.ReconcileStep(context.Background(), d, s)
						if err != nil || control.State != expectedStates[i] {
							t.Fatalf("bad control %d: got=%s err=%v want=%s", i, control.State, err, expectedStates[i])
						}
					}
					pushes := m.remote.pushCount()
					if route == "recovery" {
						if err := manager.recoverOne(context.Background(), d); err != nil {
							t.Fatal(err)
						}
					} else {
						manager.step(d)
					}
					got := m.mustDelivery()
					if tc.second == "uncertain" {
						parked := steps[1]
						if tc.first == "uncertain" {
							parked = steps[0]
						}
						if got.Phase != "needs_check" || got.BlockCode != "recovery_uncertain" || stepByID(t, m, parked.ID).Phase != "uncertain" {
							t.Fatalf("uncertainty ignored: phase=%s code=%s parked=%s pushes=%d", got.Phase, got.BlockCode, stepByID(t, m, parked.ID).Phase, m.remote.pushCount()-pushes)
						}
						if route == "live" && stepByID(t, m, steps[1].ID).Phase != "uncertain" {
							t.Fatal("live batch left a classified unknown running")
						}
						if m.remote.pushCount() != pushes || m.agent.repairsCount() != 0 {
							t.Fatal("ambiguous batch caused push/repair")
						}
						if tc.first == "uncertain" {
							m.remote.inject(steps[1].InputFingerprint, ref)
							pushes = m.remote.pushCount() // external injection is not a product push
						}
						// A repeated recovery must not use an earlier leftover observed intent
						// to leave the unresolved needs_check state.
						if err := manager.recoverOne(context.Background(), *got); err != nil {
							t.Fatal(err)
						}
						if m.mustDelivery().Phase != "needs_check" || m.remote.pushCount() != pushes {
							t.Fatal("recovery silently resumed a parked ambiguous batch")
						}
						return
					}
					for i, state := range expectedStates {
						want := "intent"
						if state == "observed" {
							want = "observed"
						}
						if gotStep := stepByID(t, m, steps[i].ID); gotStep.Phase != want {
							t.Fatalf("pending %d not durably reconciled: %s want %s", i, gotStep.Phase, want)
						}
					}
					anyObserved := tc.first == "observed" || tc.second == "observed"
					if anyObserved {
						if got.Phase != kind+"_ci" || m.remote.pushCount() != pushes {
							t.Fatalf("observed batch not reused: phase=%s newPushes=%d", got.Phase, m.remote.pushCount()-pushes)
						}
						anchor := steps[0]
						if tc.first != "observed" {
							anchor = steps[1]
						}
						want := anchor.StartedAt.Add(time.Duration(d.Policy.CISeconds) * time.Second)
						if !got.CIDeadline.Equal(want) {
							t.Fatalf("batch extended original CI budget: %v want %v", got.CIDeadline, want)
						}
					} else {
						// Both routes return fresh durable input before retrying once.
						for i := 0; i < 2 && got.Phase == "pushing_"+kind; i++ {
							manager.step(*got)
							got = m.mustDelivery()
						}
						if got.Phase != kind+"_ci" || m.remote.pushCount() != pushes+1 {
							t.Fatalf("proven-unstarted batch not retried exactly once: %s pushes=%d", got.Phase, m.remote.pushCount()-pushes)
						}
					}
				})
			}
		}
	}
}

type receiptCutClock struct {
	Clock
	calls int
	cut   func()
}

func (c *receiptCutClock) Now() time.Time {
	c.calls++
	if c.calls == 2 {
		c.cut()
	}
	return c.Clock.Now()
}

// Force a real Store CAS conflict between a metadata receipt and the final
// phase+anchor receipt. This models an interrupted batch without a new hook.
func TestPendingPushBatchRetainsAnchorUntilPhaseCAS(t *testing.T) {
	for _, kind := range []string{"task", "main"} {
		for _, resume := range []string{"live", "recovery"} {
			t.Run(kind+"/"+resume, func(t *testing.T) {
				m, manager, d := recoveryStageFixture(t)
				d.Phase = "pushing_" + kind
				ref := "refs/heads/kaiwu/task-" + d.TaskID
				if kind == "main" {
					d.LocalMainSHA = d.CandidateSHA
					ref = "refs/heads/main"
				}
				var steps [2]workbench.DeliveryStep
				for i := range steps {
					steps[i] = workbench.DeliveryStep{ID: fmt.Sprintf("anchor-%d", i), DeliveryID: d.ID, Kind: "push_" + kind, Phase: "running",
						InputFingerprint: d.CandidateSHA, BeforeSHA: m.baseSHA, Ref: ref, StartedAt: m.clock.Now().Add(time.Duration(i-2) * time.Second)}
					var err error
					d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, &steps[i])
					if err != nil {
						t.Fatal(err)
					}
				}
				m.remote.inject(d.CandidateSHA, ref)
				pushes := m.remote.pushCount()
				cut := &receiptCutClock{Clock: m.clock, cut: func() {
					current := m.mustDelivery()
					if _, err := m.store.AdvanceDelivery(current.ID, current.Version, *current, nil); err != nil {
						t.Fatal(err)
					}
				}}
				manager.cfg.Clock = cut
				_, handled, cont := manager.resolvePendingIntent(d, "push_"+kind)
				if !handled || cont || cut.calls != 2 {
					t.Fatalf("CAS cut was ignored: handled=%v cont=%v calls=%d", handled, cont, cut.calls)
				}
				got := m.mustDelivery()
				if got.Phase != "pushing_"+kind || stepByID(t, m, steps[0].ID).Phase != "running" || stepByID(t, m, steps[1].ID).Phase != "observed" || m.remote.pushCount() != pushes {
					t.Fatalf("lost durable anchor at incomplete phase CAS: phase=%s first=%s second=%s", got.Phase, stepByID(t, m, steps[0].ID).Phase, stepByID(t, m, steps[1].ID).Phase)
				}
				manager.cfg.Clock = m.clock
				if resume == "live" {
					_, handled, cont = manager.resolvePendingIntent(*got, "push_"+kind)
					if !handled || !cont {
						t.Fatal("known pending anchor could not be reconciled")
					}
				} else if err := manager.recoverOne(context.Background(), *got); err != nil {
					t.Fatal(err)
				}
				got = m.mustDelivery()
				want := steps[0].StartedAt.Add(time.Duration(d.Policy.CISeconds) * time.Second)
				if got.Phase != kind+"_ci" || !got.CIDeadline.Equal(want) || stepByID(t, m, steps[0].ID).Phase != "observed" || m.remote.pushCount() != pushes {
					t.Fatalf("partial batch was not resumed without push/budget reset: phase=%s deadline=%v want=%v", got.Phase, got.CIDeadline, want)
				}
			})
		}
	}
}

func TestPushReceiptKeepsIntentDeadline(t *testing.T) {
	for _, kind := range []string{"task", "main"} {
		t.Run(kind, func(t *testing.T) {
			m, manager, d := recoveryStageFixture(t)
			d.Phase = "pushing_task"
			if kind == "main" {
				gitIn(t, m.source, "merge", "--ff-only", d.CandidateSHA)
				d.Phase, d.LocalMainSHA = "pushing_main", d.CandidateSHA
			}
			var err error
			d, err = m.store.AdvanceDelivery(d.ID, d.Version, d, nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := m.remote.transport(m.store, m.taskID)
			manager.cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, ref string, lease gitops.ProcessLease) (string, error) {
				if args[0] == "push" {
					m.clock.advance(7 * time.Millisecond)
				}
				return transport(ctx, dir, target, args, ref, lease)
			})})
			var next workbench.Delivery
			var cont bool
			if kind == "main" {
				next, cont = manager.stagePushingMain(d)
			} else {
				next, cont = manager.stagePushingTask(d)
			}
			if !cont || next.Phase != kind+"_ci" {
				t.Fatalf("push fixture failed: phase=%s code=%s", next.Phase, next.BlockCode)
			}
			page, err := m.store.DeliverySteps(d.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			var receipt workbench.DeliveryStep
			for _, s := range page.Items {
				if s.Kind == "push_"+kind && s.InputFingerprint == next.CandidateSHA {
					receipt = s
				}
			}
			want := receipt.StartedAt.Add(time.Duration(d.Policy.CISeconds) * time.Second)
			if !next.CIDeadline.Equal(want) {
				t.Fatalf("receipt moved persisted CI origin: got=%v want=%v", next.CIDeadline, want)
			}
		})
	}
}
