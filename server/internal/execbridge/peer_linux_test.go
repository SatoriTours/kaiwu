//go:build linux

package execbridge

import (
	"context"
	"os"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type simulatedUnit struct{ state PeerState }

func (s simulatedUnit) InspectBridge(context.Context, executionenv.Identity) (PeerState, error) {
	return s.state, nil
}
func TestNativePeerPIDFDAndMainPID(t *testing.T) {
	if os.Getenv("KAIWU_TEST_PEERPIDFD") != "1" {
		t.Skip("explicit local kernel socket/pidfd probe; no systemd units")
	}
	_, _, identity := authFixture(t)
	left, right := unixPair(t)
	defer left.Close()
	defer right.Close()
	observed := PeerState{Observation: executionenv.Observation{Identity: identity, State: executionenv.OwnedRunning, GroupPopulated: true}, MainPID: os.Getpid()}
	verifier := NativeVerifier{Expected: identity, Inspector: simulatedUnit{observed}}
	peer, err := verifier.Pin(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := peer.Inspect(context.Background()); err != nil || got != identity {
		t.Fatalf("kernel peer pin failed: %v %v", got, err)
	}
	peer.Close()
	if _, err := peer.Inspect(context.Background()); err == nil {
		t.Fatal("closed pidfd reused")
	}
	observed.MainPID++
	verifier.Inspector = simulatedUnit{observed}
	foreign, err := verifier.Pin(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	if _, err := foreign.Inspect(context.Background()); err == nil {
		t.Fatal("non-main same-UID peer accepted")
	}
}
func TestNativePeerHasNoUnsupportedFallback(t *testing.T) {
	_, _, id := authFixture(t)
	left, right := unixPair(t)
	defer left.Close()
	defer right.Close()
	if peer, err := (NativeVerifier{Expected: id}).Pin(context.Background(), left); err == nil {
		peer.Close()
		t.Fatal("missing inspector accepted")
	}
}
