//go:build linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"net"
	"os"
	"testing"
	"time"
)

func runtimeConnect(t *testing.T, r *BridgeRuntime, f *sessionFixture) (*HostChannel, <-chan error) {
	t.Helper()
	left, right := unixPair(t)
	done := make(chan error, 1)
	go func() { done <- r.ServeConnection(context.Background(), right) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	h, err := AuthenticateHost(ctx, left, f.bootstrap, f.key, fixedVerifier{&fixedPeer{identity: f.identity}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Detach() })
	return h, done
}
func TestBridgeRuntimeRetainsTaskAcrossControlConnections(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	h.Detach()
	b.Detach()
	r, err := NewBridgeRuntime(f.bootstrap, a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupBridgeRuntime(r) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first, done := runtimeConnect(t, r, f)
	if err = first.Prepare(ctx, f.files, f.expect); err != nil {
		t.Fatal(err)
	}
	observe := func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	}
	remote, err := OpenRemote(ctx, first, observe)
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Commit(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err = remote.Input(ctx, 1, []byte("before\n")); err != nil {
		t.Fatal(err)
	}
	remote.Detach()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	second, done2 := runtimeConnect(t, r, f)
	again, err := OpenRemote(ctx, second, observe)
	if err != nil {
		t.Fatal(err)
	}
	status, err := again.Status(ctx)
	if err != nil || !status.Executed {
		t.Fatal("reconnect lost original session")
	}
	if err = again.Input(ctx, 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	var after uint64
	for n := 0; n < 20; n++ {
		frames, e := again.Events(ctx, after, true)
		if e != nil {
			t.Fatal(e)
		}
		exited := false
		for _, frame := range frames {
			after = frame.Sequence
			if frame.Kind == ProcessExit {
				exited = true
			}
		}
		if exited {
			break
		}
		if n == 19 {
			t.Fatal("no child exit")
		}
	}
	again.Detach()
	<-done2
	raw, err := os.ReadFile(f.marker)
	if err != nil || string(raw) != "spawn\nbefore\nexit\n" {
		t.Fatalf("replayed/restarted/EOF: %q %v", raw, err)
	}
	if err = r.CloseAfterGroupGone(executionenv.Observation{Identity: f.identity, State: executionenv.OwnedGone}); err != nil {
		t.Fatal(err)
	}
}
func TestBridgeRuntimeRejectsMissingAttestorAndUnprovenClose(t *testing.T) {
	b, _, _ := authFixture(t)
	if _, err := NewBridgeRuntime(b, nil); err == nil {
		t.Fatal("nil attestor accepted")
	}
	f, h, bridge, a := prepareHandshakeFixture(t)
	h.Detach()
	bridge.Detach()
	r, err := NewBridgeRuntime(f.bootstrap, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.CloseAfterGroupGone(executionenv.Observation{}); err == nil {
		t.Fatal("unproven close")
	}
}

func cleanupBridgeRuntime(r *BridgeRuntime) {
	r.mu.Lock()
	s := r.session
	r.mu.Unlock()
	if s != nil {
		s.testCleanup()
	}
}
func TestBridgeRuntimeLostPreparedReceiptRetainsAndReattaches(t *testing.T) {
	f, h, b, a := prepareHandshakeFixture(t)
	h.Detach()
	b.Detach()
	drop := make(chan *HostChannel, 1)
	r, err := NewBridgeRuntime(f.bootstrap, handshakeAttestor{base: a, hook: func(context.Context) { (<-drop).Detach() }})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupBridgeRuntime(r)
	first, done := runtimeConnect(t, r, f)
	drop <- first
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if first.Prepare(ctx, f.files, f.expect) == nil {
		t.Fatal("lost receipt returned success")
	}
	if <-done == nil {
		t.Fatal("runtime hid lost receipt")
	}
	f.files[1].Close()
	lock, err := os.OpenFile(f.files[1].Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if lockErr(lock) == nil {
		t.Fatal("runtime error released writer")
	}
	next, served := runtimeConnect(t, r, f)
	remote, err := OpenRemote(ctx, next, func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := remote.Status(ctx)
	if err != nil || status.Attempted || status.Executed {
		t.Fatal("reattach replayed initialization or launch")
	}
	bundle, err := remote.RecoverFiles(ctx, f.expect)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Close()
	remote.Detach()
	<-served
	if _, err = os.Stat(f.marker); !os.IsNotExist(err) {
		t.Fatal("reconnect executed task")
	}
	if err = r.CloseAfterGroupGone(executionenv.Observation{Identity: f.identity, State: executionenv.OwnedGone}); err != nil {
		t.Fatal(err)
	}
	if lockErr(lock) != nil {
		t.Fatal("group-gone did not release retained lease")
	}
	left, right := net.Pipe()
	defer left.Close()
	if r.ServeConnection(ctx, right) == nil {
		t.Fatal("closed runtime reused")
	}
}
