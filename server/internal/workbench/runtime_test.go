package workbench

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/execbridge"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func runtimeStoreFixture(t *testing.T) (*Store, string, executionenv.Record) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "runtime.db")
	s, err := Open(db, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	err = s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p", StorageMode: "managed"}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", SessionID: "original-session", SessionFile: "original.jsonl", Messages: []Message{{ID: "m", Text: "keep"}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := executionenv.Record{Identity: executionenv.Identity{RunID: strings.Repeat("1", 32), BootID: "01234567-89ab-cdef-0123-456789abcdef", Unit: "kaiwu-exec-" + strings.Repeat("1", 32) + ".service"}, Owner: executionenv.Owner{ProjectID: "p", ConversationID: "c"}, Kind: "pi", SpecHash: strings.Repeat("a", 64), Budget: executionenv.Budget{CPUQuotaUSec: 25000, MemoryBytes: 96 << 20, PidsMax: 32, DiskBytes: 4096}, Phase: executionenv.Starting}
	return s, db, r
}
func TestCrashBetweenStartAndReceiptKeepsUnresolvedClaim(t *testing.T) {
	ctx := context.Background()
	s, db, r := runtimeStoreFixture(t)
	saved, err := s.ReserveRuntime(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	saved.Phase = executionenv.Unknown
	saved, err = s.AdvanceRuntime(ctx, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(db, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replay, err := reopened.ReserveRuntime(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Phase != executionenv.Unknown || replay.Version != saved.Version {
		t.Fatal("ambiguous start was reset")
	}
	active, err := reopened.ActiveRuntimes(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("lost unresolved claim: %v %v", active, err)
	}
	conv := reopened.Snapshot().Conversations["c"]
	if conv.SessionID != "original-session" || conv.SessionFile != "original.jsonl" || len(conv.Messages) != 1 {
		t.Fatal("runtime migration replaced original session")
	}
}
func TestRuntimeReservationPreventsTwoStoreWriters(t *testing.T) {
	s, db, r := runtimeStoreFixture(t)
	other, err := Open(db, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, store := range []*Store{s, other} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			next := r
			if i == 1 {
				next.Identity.RunID = strings.Repeat("2", 32)
				next.Identity.Unit = "kaiwu-exec-" + next.Identity.RunID + ".service"
			}
			_, err := store.ReserveRuntime(context.Background(), next)
			results <- err
		}(i, store)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, executionenv.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("duplicate writer admitted: success=%d conflicts=%d", success, conflicts)
	}
}
func TestRuntimeCASAndGroupGoneAreRequired(t *testing.T) {
	s, _, r := runtimeStoreFixture(t)
	ctx := context.Background()
	saved, err := s.ReserveRuntime(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	stale := saved
	saved.Phase = executionenv.Unknown
	saved, err = s.AdvanceRuntime(ctx, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	stale.Phase = executionenv.Unknown
	if _, err := s.AdvanceRuntime(ctx, stale, nil); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
	stopped := saved
	stopped.Phase = executionenv.Stopped
	foreign := stopped.Identity
	foreign.BootID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if _, err := s.AdvanceRuntime(ctx, stopped, &executionenv.Observation{State: executionenv.OwnedGone, Identity: foreign}); err == nil {
		t.Fatal("foreign exit released claim")
	}
	duplicate := r
	duplicate.Identity.RunID = strings.Repeat("2", 32)
	duplicate.Identity.Unit = "kaiwu-exec-" + duplicate.Identity.RunID + ".service"
	if _, err := s.ReserveRuntime(ctx, duplicate); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatal("unknown phase released live slot")
	}
	if _, err := s.AdvanceRuntime(ctx, stopped, &executionenv.Observation{State: executionenv.OwnedGone, Identity: stopped.Identity}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntime(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
}
func TestActiveRuntimeOwnerCannotBeDeletedOrRebound(t *testing.T) {
	s, _, r := runtimeStoreFixture(t)
	if _, err := s.ReserveRuntime(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*State){func(st *State) { delete(st.Projects, "p") }, func(st *State) { delete(st.Conversations, "c") }, func(st *State) { c := st.Conversations["c"]; c.ProjectID = "other"; st.Conversations["c"] = c }} {
		if err := s.Update(func(st *State) error { change(st); return nil }); err == nil {
			t.Fatal("live runtime owner removed")
		}
	}
	if s.Snapshot().Conversations["c"].ProjectID != "p" {
		t.Fatal("failed owner edit persisted")
	}
}
func TestRuntimeCannotClaimLegacyOrForeignOwner(t *testing.T) {
	s, _, r := runtimeStoreFixture(t)
	r.Owner.ProjectID = "other"
	if _, err := s.ReserveRuntime(context.Background(), r); err == nil {
		t.Fatal("foreign owner accepted")
	}
	r.Owner.ProjectID = "p"
	if err := s.Update(func(st *State) error {
		p := st.Projects["p"]
		p.StorageMode = "legacy"
		st.Projects["p"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveRuntime(context.Background(), r); err == nil {
		t.Fatal("legacy owner gained managed runtime")
	}
}

type runtimeEndpoint struct {
	identity executionenv.Identity
	commits  int
	executed bool
	lost     bool
	store    *Store
	testing  *testing.T
}

func (e *runtimeEndpoint) Identity() executionenv.Identity { return e.identity }
func (e *runtimeEndpoint) Commit(ctx context.Context, seq uint64) error {
	e.commits++
	row, err := e.store.Runtime(ctx, e.identity.RunID)
	if err != nil || row.Phase != executionenv.CommitIntent || row.LaunchSequence != 1 || seq != 1 {
		e.testing.Fatal("execution attempted before durable commit-intent")
	}
	e.executed = true
	if e.lost {
		return errors.New("synthetic lost acknowledgement")
	}
	return nil
}
func (e *runtimeEndpoint) Status(context.Context) (execbridge.LaunchStatus, error) {
	return execbridge.LaunchStatus{Attempted: e.executed, Executed: e.executed}, nil
}
func (e *runtimeEndpoint) Observe(context.Context) (executionenv.Observation, error) {
	return executionenv.Observation{Identity: e.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
}
func TestCommitReceiptLossAndAttachNeverReplayLaunch(t *testing.T) {
	s, _, r := runtimeStoreFixture(t)
	ctx := context.Background()
	saved, err := s.ReserveRuntime(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	saved.Identity.InvocationID = strings.Repeat("b", 32)
	saved.Identity.ControlGroup = "/user.slice/" + saved.Identity.Unit
	saved.RendezvousInode = 123
	saved.Phase = executionenv.Prepared
	saved, err = s.AdvanceRuntime(ctx, saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &runtimeEndpoint{identity: saved.Identity, store: s, testing: t, lost: true}
	if _, err := execbridge.CommitPrepared(ctx, s, saved.Identity.RunID, endpoint); err == nil {
		t.Fatal("lost acknowledgement reported running")
	}
	unresolved, err := s.Runtime(ctx, saved.Identity.RunID)
	if err != nil || unresolved.Phase != executionenv.Unknown || unresolved.LaunchSequence != 1 {
		t.Fatal("ambiguous execution was forgotten")
	}
	restored, err := execbridge.RecoverLaunch(ctx, s, saved.Identity.RunID, endpoint)
	if err != nil || restored.Phase != executionenv.Running {
		t.Fatalf("attach failed: %v %v", restored, err)
	}
	if endpoint.commits != 1 {
		t.Fatal("attach replayed the original execution")
	}
	if _, err := execbridge.CommitPrepared(ctx, s, saved.Identity.RunID, endpoint); err == nil || endpoint.commits != 1 {
		t.Fatal("duplicate commit reached endpoint")
	}
}
func TestCrashAfterCommitIntentBeforeSendNeverExecutesOnRecovery(t *testing.T) {
	s, _, r := runtimeStoreFixture(t)
	ctx := context.Background()
	row, err := s.ReserveRuntime(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	row.Identity.InvocationID = strings.Repeat("b", 32)
	row.Identity.ControlGroup = "/user.slice/" + row.Identity.Unit
	row.RendezvousInode = 123
	row.Phase = executionenv.Prepared
	row, err = s.AdvanceRuntime(ctx, row, nil)
	if err != nil {
		t.Fatal(err)
	}
	row.Phase = executionenv.CommitIntent
	row.LaunchSequence = 1
	row, err = s.AdvanceRuntime(ctx, row, nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &runtimeEndpoint{identity: row.Identity, store: s, testing: t}
	recovered, err := execbridge.RecoverLaunch(ctx, s, row.Identity.RunID, endpoint)
	if err == nil || recovered.Phase != executionenv.Unknown || endpoint.commits != 0 {
		t.Fatalf("recovery executed old intent: phase=%s commits=%d err=%v", recovered.Phase, endpoint.commits, err)
	}
}
