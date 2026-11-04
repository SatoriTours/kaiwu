//go:build linux

package execbridge

import (
	"context"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func prepareHandshakeFixture(t *testing.T) (*sessionFixture, *HostChannel, *BridgeChannel, syntheticAttestor) {
	t.Helper()
	f := newSessionFixture(t, 16384, "echo", nil)
	f.s.testCleanup()
	f.host.Detach()
	f.bridge.Detach()
	if err := f.files[len(f.files)-1].Truncate(0); err != nil {
		t.Fatal(err)
	}
	h, b := sessionPair(t, f.bootstrap, f.key, f.identity)
	t.Cleanup(func() { h.Detach(); b.Detach() })
	caps := executionenv.Capabilities{Platform: "linux", EvidenceID: "SYNTHETIC-not-native", PrivateIPC: true, SocketFilter: true, UserManager: true, PrivateRoot: true, PrivateUsers: true, PrivatePID: true, PrivateNetwork: true, CPU: true, Memory: true, Pids: true, Disk: executionenv.DiskObservation{ProjectID: "p", Root: f.config.Spec.ProjectRoot, EvidenceID: "SYNTHETIC-not-quota", HardLimit: 1 << 20, CoversAllWrites: true}}
	return f, h, b, syntheticAttestor{caps}
}
func TestPrepareHandshakeTransfersFDsButNeverExecutes(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Prepare(ctx, f.files, f.expect) }()
	s, err := b.AcceptPrepare(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	defer s.testCleanup()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.marker); !os.IsNotExist(err) || s.State().Attempted {
		t.Fatal("PREPARED executed task")
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	served := make(chan error, 1)
	go func() { served <- s.Serve(serveCtx, b) }()
	remote, err := OpenRemote(ctx, h, func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Commit(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err = remote.Input(ctx, 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	remote.Detach()
	<-served
}
func TestPrepareHandshakeMissingQuotaNeverAcknowledgesPrepared(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	a.caps.Disk = executionenv.DiskObservation{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Prepare(ctx, f.files, f.expect) }()
	s, err := b.AcceptPrepare(ctx, a)
	if err == nil || s != nil {
		t.Fatal("quota-free session prepared")
	}
	if err = <-done; err == nil {
		t.Fatal("host got false prepared receipt")
	}
	stat, err := f.files[len(f.files)-1].Stat()
	if err != nil || stat.Size() != 0 {
		t.Fatal("gate failure wrote spool")
	}
	if _, err = os.Stat(f.marker); !os.IsNotExist(err) {
		t.Fatal("gate failure executed child")
	}
}
func TestPrepareHandshakeRejectsNilAttestorAndBadBundle(t *testing.T) {
	f, h, b, _ := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s, err := b.AcceptPrepare(ctx, nil); s != nil || !errors.Is(err, executionenv.ErrUnavailable) {
		t.Fatal("nil attestor accepted")
	}
	if err := h.Prepare(ctx, f.files, BundleExpectation{}); err == nil {
		t.Fatal("invalid FD identities accepted")
	}
}

type handshakeAttestor struct {
	base syntheticAttestor
	hook func(context.Context)
}

func (a handshakeAttestor) Attest(ctx context.Context, s executionenv.Spec, id executionenv.Identity) (executionenv.Capabilities, error) {
	a.hook(ctx)
	return a.base.Attest(ctx, s, id)
}
func TestPrepareHandshakeReceiptLossRetainsSessionLease(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Prepare(ctx, f.files, f.expect) }()
	s, err := b.AcceptPrepare(ctx, handshakeAttestor{base: a, hook: func(context.Context) { h.Detach() }})
	if err == nil || s == nil {
		t.Fatal("lost receipt dropped prepared session or reported success")
	}
	defer s.testCleanup()
	if err = <-done; err == nil {
		t.Fatal("lost receipt reported host success")
	}
	f.files[1].Close()
	lock, err := os.OpenFile(f.files[1].Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if lockErr(lock) == nil {
		t.Fatal("lost receipt released retained writer lease")
	}
	if s.State().Attempted {
		t.Fatal("lost PREPARED receipt caused launch")
	}
}
func TestPrepareHandshakeAttestationSharesCallerBudget(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Prepare(ctx, f.files, f.expect) }()
	start := time.Now()
	s, err := b.AcceptPrepare(ctx, handshakeAttestor{base: a, hook: func(c context.Context) {
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("renewed handshake timeout")
		}
		<-c.Done()
	}})
	if err == nil || s != nil || time.Since(start) > time.Second {
		t.Fatal("attestation escaped caller budget")
	}
	if err = <-done; err == nil {
		t.Fatal("canceled handshake reported success")
	}
}
func TestPrepareHandshakeRejectsForeignOfferBeforeFDs(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	foreign := f.identity
	foreign.RunID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	foreign.Unit = "kaiwu-exec-" + foreign.RunID + ".service"
	foreign.ControlGroup = "/user.slice/" + foreign.Unit
	sent := make(chan error, 1)
	go func() {
		sent <- writeWire(h.conn, prepareOffer{Version: 1, Identity: foreign, Bundle: f.expect}, 16384)
	}()
	if s, err := b.AcceptPrepare(ctx, a); s != nil || err == nil {
		t.Fatal("foreign run accepted")
	}
	<-sent
	if st, err := f.files[len(f.files)-1].Stat(); err != nil || st.Size() != 0 {
		t.Fatal("foreign offer touched spool")
	}
}

func TestPrepareHandshakeSQLitePreparedThenCommitIntent(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	journal, err := workbench.Open(filepath.Join(t.TempDir(), "runtime.db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err = journal.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", StorageMode: "managed"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", ProjectID: "p", TaskID: "t", SessionID: "original", SessionFile: "original.jsonl"}
		st.Tasks["t"] = workbench.Task{ID: "t", ProjectID: "p", ConversationID: "c", ExecutionID: "e"}
		st.Executions["e"] = workbench.Execution{ID: "e", TaskID: "t", ConversationID: "c"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	row, err := journal.ReserveRuntime(ctx, executionenv.Record{Identity: f.bootstrap.Identity, Owner: f.config.Spec.Owner, Kind: "pi", SpecHash: f.bootstrap.SpecHash, Budget: f.config.Spec.Budget, Writer: true, Phase: executionenv.Starting})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Prepare(ctx, f.files, f.expect) }()
	s, err := b.AcceptPrepare(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	defer s.testCleanup()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	// The application still owns this persistence step. A transport receipt alone
	// neither updates the journal nor creates launch intent.
	saved, err := journal.Runtime(ctx, row.Identity.RunID)
	if err != nil || saved.Phase != executionenv.Starting || saved.LaunchSequence != 0 {
		t.Fatal("transport bypassed durable state")
	}
	row.Identity = f.identity
	row.RendezvousInode = 1
	row.Phase = executionenv.Prepared
	row, err = journal.AdvanceRuntime(ctx, row, nil)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	served := make(chan error, 1)
	go func() { served <- s.Serve(serveCtx, b) }()
	remote, err := OpenRemote(ctx, h, func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = CommitPrepared(ctx, journal, row.Identity.RunID, remote)
	if err != nil || row.Phase != executionenv.Running || row.LaunchSequence != 1 {
		t.Fatal("durable commit integration failed", err)
	}
	if err = remote.Input(ctx, 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	remote.Detach()
	<-served
	if c := journal.Snapshot().Conversations["c"]; c.SessionID != "original" || c.SessionFile != "original.jsonl" {
		t.Fatal("transport rebound business session")
	}
}
