//go:build linux

package execbridge

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func serveFixture(t *testing.T, f *sessionFixture, h *HostChannel, b *BridgeChannel) (*Remote, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- f.s.Serve(ctx, b) }()
	call, cancelCall := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCall()
	remote, err := OpenRemote(call, h, func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return remote, done
}
func TestWireDetachAttachKeepsRealChildAndNeverReplays(t *testing.T) {
	f := newSessionFixture(t, 32768, "echo", nil)
	remote, done := serveFixture(t, f, f.host, f.bridge)
	if err := remote.Commit(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := remote.Input(context.Background(), 1, []byte("wire-first\n")); err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, f.s, "echo:wire-first")
	remote.Detach()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("detach left command loop blocked")
	}
	host, bridge := sessionPair(t, f.bootstrap, f.key, f.identity)
	next, nextDone := serveFixture(t, f, host, bridge)
	state, err := next.Status(context.Background())
	if err != nil || !state.Executed || state.Gap {
		t.Fatalf("attach lost running process: %+v %v", state, err)
	}
	if err := next.Input(context.Background(), 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := f.s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(f.marker)
	if err != nil || string(content) != "spawn\nwire-first\nexit\n" {
		t.Fatalf("wire re-executed: %q %v", content, err)
	}
	frames, err := next.Events(ctx, 0, false)
	if err != nil || len(frames) == 0 {
		t.Fatalf("replay stream unavailable: %v", err)
	}
	next.Detach()
	<-nextDone
}
func TestWireRejectsUnknownAndOldEpochWithoutStoppingChild(t *testing.T) {
	f := newSessionFixture(t, 16384, "echo", nil)
	remote, done := serveFixture(t, f, f.host, f.bridge)
	if err := remote.Commit(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.call(context.Background(), wireRequest{Op: "input", Epoch: remote.epoch - 1, Sequence: 1, Data: []byte("bad\n")}); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatal("old epoch accepted")
	}
	if _, err := remote.call(context.Background(), wireRequest{Op: "unsupported", Epoch: remote.epoch}); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatal("unknown operation accepted")
	}
	if err := remote.Input(context.Background(), 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := f.s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	remote.Detach()
	<-done
}
