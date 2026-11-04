//go:build linux

package execbridge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

func TestAttachRecoversLeaseWithoutReacquire(t *testing.T) {
	f := newSessionFixture(t, 32768, "echo", nil)
	remote, done := serveFixture(t, f, f.host, f.bridge)
	if err := remote.Commit(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	remote.Detach()
	<-done
	name := f.files[1].Name()
	f.files[1].Close()
	host, bridge := sessionPair(t, f.bootstrap, f.key, f.identity)
	next, nextDone := serveFixture(t, f, host, bridge)
	returned, err := next.RecoverFiles(context.Background(), f.expect)
	if err != nil {
		t.Fatal(err)
	}
	defer returned.Close()
	probe, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatal("lease lost during recovery")
	}
	if err := next.Input(context.Background(), 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := f.s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	next.Detach()
	<-nextDone
	// Close the bridge's exact copy after its trusted fixture group-gone proof.
	proof := goneFixtureProof(f)
	if err := f.s.CloseAfterGroupGone(proof); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatal("returned OFD did not continue original lease")
	}
	returned.Close()
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("last returned OFD did not release")
	}
}
func TestGapBridgeStillAttachesForStatusAndLease(t *testing.T) {
	f := newSessionFixture(t, 256, "overflow", nil)
	if err := f.s.Commit(context.Background(), f.bridge, f.epoch, 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = f.s.Wait(ctx)
	f.host.Detach()
	f.bridge.Detach()
	host, bridge := sessionPair(t, f.bootstrap, f.key, f.identity)
	remote, done := serveFixture(t, f, host, bridge)
	state, err := remote.Status(ctx)
	if err != nil || !state.Gap {
		t.Fatal("gap status lost on attach")
	}
	retained, err := remote.RecoverFiles(ctx, f.expect)
	if err != nil {
		t.Fatal(err)
	}
	retained.Close()
	remote.Detach()
	<-done
}

func goneFixtureProof(f *sessionFixture) executionenv.Observation {
	return executionenv.Observation{Identity: f.identity, State: executionenv.OwnedGone}
}
