package execbridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func authFixture(t *testing.T) (Bootstrap, ed25519.PrivateKey, executionenv.Identity) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	i := executionenv.Identity{RunID: strings.Repeat("1", 32), BootID: "01234567-89ab-cdef-0123-456789abcdef", Unit: "kaiwu-exec-" + strings.Repeat("1", 32) + ".service", InvocationID: strings.Repeat("2", 32)}
	i.ControlGroup = "/user.slice/" + i.Unit
	return Bootstrap{Identity: executionenv.Identity{RunID: i.RunID, BootID: i.BootID, Unit: i.Unit}, SpecHash: strings.Repeat("a", 64), HostPublicKey: pub}, key, i
}

// This double models the OS boundary only. It does NOT prove SO_PEERPIDFD,
// MainPID, user namespace or a real systemd unit; those remain opt-in work.
type fixedPeer struct {
	identity executionenv.Identity
	second   executionenv.Identity
	reads    atomic.Int32
	closed   atomic.Bool
}

func (p *fixedPeer) Inspect(context.Context) (executionenv.Identity, error) {
	if p.reads.Add(1) > 1 && p.second.RunID != "" {
		return p.second, nil
	}
	return p.identity, nil
}
func (p *fixedPeer) Close() error { p.closed.Store(true); return nil }

type fixedVerifier struct{ peer *fixedPeer }

func (v fixedVerifier) Pin(context.Context, net.Conn) (PinnedPeer, error) { return v.peer, nil }
func TestBridgeAuthenticatesBothSidesWithFreshChallenges(t *testing.T) {
	b, key, id := authFixture(t)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	peer := &fixedPeer{identity: id}
	bridgeResult := make(chan error, 1)
	go func() {
		ch, err := AuthenticateBridge(ctx, right, b)
		if err == nil {
			err = ch.Detach()
		}
		bridgeResult <- err
	}()
	host, err := AuthenticateHost(ctx, left, b, key, fixedVerifier{peer})
	if err != nil {
		t.Fatal(err)
	}
	if host.Identity() != id || peer.reads.Load() != 2 {
		t.Fatal("peer not fixed and checked twice")
	}
	if err := host.Detach(); err != nil {
		t.Fatal(err)
	}
	if err := <-bridgeResult; err != nil {
		t.Fatal(err)
	}
	if !peer.closed.Load() {
		t.Fatal("pinned peer handle leaked")
	}
}
func TestSameUIDCannotWinBootstrapWithForeignIdentity(t *testing.T) {
	b, key, id := authFixture(t)
	id.InvocationID = strings.Repeat("b", 32)
	id.RunID = strings.Repeat("3", 32)
	l, r := net.Pipe()
	defer l.Close()
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := AuthenticateHost(ctx, l, b, key, fixedVerifier{&fixedPeer{identity: id}}); !errors.Is(err, ErrAuth) {
		t.Fatalf("foreign peer accepted: %v", err)
	}
	if _, err := AuthenticateHost(ctx, l, b, key, nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("absent verifier accepted: %v", err)
	}
}
func TestPeerInvocationChangeDuringHandshakeIsRejected(t *testing.T) {
	b, key, id := authFixture(t)
	changed := id
	changed.InvocationID = strings.Repeat("c", 32)
	l, r := net.Pipe()
	defer l.Close()
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := AuthenticateBridge(ctx, r, b); done <- err }()
	if ch, err := AuthenticateHost(ctx, l, b, key, fixedVerifier{&fixedPeer{identity: id, second: changed}}); !errors.Is(err, ErrAuth) {
		if ch != nil {
			ch.Detach()
		}
		t.Fatalf("rebound invocation accepted: %v", err)
	}
	<-done
}
func TestWrongHostKeyAndReplayedTranscriptAreRejected(t *testing.T) {
	b, key, _ := authFixture(t)
	host := make([]byte, 32)
	bridge := make([]byte, 32)
	host[0] = 1
	bridge[0] = 2
	sig, err := SignChallenge(key, b, host, bridge)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyChallenge(b, host, bridge, sig); err != nil {
		t.Fatal(err)
	}
	bridge[0] = 3
	if err := VerifyChallenge(b, host, bridge, sig); !errors.Is(err, ErrAuth) {
		t.Fatal("old transcript replayed")
	}
	bridge[0] = 2
	b.SpecHash = strings.Repeat("b", 64)
	if err := VerifyChallenge(b, host, bridge, sig); !errors.Is(err, ErrAuth) {
		t.Fatal("signature crossed spec")
	}
	if _, err := SignChallenge(key, b, make([]byte, 32), bridge); !errors.Is(err, ErrAuth) {
		t.Fatal("zero challenge accepted")
	}
}
func TestAuthenticationCancellationIsBounded(t *testing.T) {
	b, key, id := authFixture(t)
	l, r := net.Pipe()
	defer l.Close()
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := AuthenticateHost(ctx, l, b, key, fixedVerifier{&fixedPeer{identity: id}})
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("cancelled authentication blocked")
	}
}

func TestWrongHostSigningKeyNeverProducesAcceptedProof(t *testing.T) {
	b, _, _ := authFixture(t)
	_, wrong, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, peer := make([]byte, 32), make([]byte, 32)
	host[0] = 1
	peer[0] = 2
	if _, err := SignChallenge(wrong, b, host, peer); !errors.Is(err, ErrAuth) {
		t.Fatal("foreign private key accepted")
	}
}

type failedPin struct{ peer *fixedPeer }

func (v failedPin) Pin(context.Context, net.Conn) (PinnedPeer, error) { return v.peer, ErrAuth }
func TestFailedPinCannotLeakKernelHandle(t *testing.T) {
	b, key, id := authFixture(t)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	peer := &fixedPeer{identity: id}
	if _, err := AuthenticateHost(context.Background(), left, b, key, failedPin{peer}); err == nil {
		t.Fatal("failed pin accepted")
	}
	if !peer.closed.Load() {
		t.Fatal("failed verifier retained peer handle")
	}
}

// Context-aware OS observers may be slower than the socket. The authentication
// cap must be propagated to them, not merely installed on the connection.
type budgetVerifier struct {
	peer     PinnedPeer
	blockPin bool
}

func (v budgetVerifier) Pin(ctx context.Context, _ net.Conn) (PinnedPeer, error) {
	if v.blockPin {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(6500 * time.Millisecond):
			return nil, ErrAuth
		}
	}
	return v.peer, nil
}

type budgetPeer struct{ closed atomic.Bool }

func (p *budgetPeer) Inspect(ctx context.Context) (executionenv.Identity, error) {
	select {
	case <-ctx.Done():
		return executionenv.Identity{}, ctx.Err()
	case <-time.After(6500 * time.Millisecond):
		return executionenv.Identity{}, ErrAuth
	}
}
func (p *budgetPeer) Close() error { p.closed.Store(true); return nil }
func TestAuthenticationBudgetCoversPinAndInspect(t *testing.T) {
	for _, name := range []string{"Pin", "Inspect"} {
		t.Run(name, func(t *testing.T) {
			b, key, _ := authFixture(t)
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			ctx := context.Background()
			if name == "Inspect" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
			}
			peer := &budgetPeer{}
			start := time.Now()
			ch, err := AuthenticateHost(ctx, left, b, key, budgetVerifier{peer: peer, blockPin: name == "Pin"})
			elapsed := time.Since(start)
			if ch != nil {
				ch.Detach()
			}
			if !errors.Is(err, ErrAuth) || elapsed > 6*time.Second {
				t.Fatalf("authentication cap omitted %s: elapsed=%s err=%v", name, elapsed, err)
			}
			if name == "Inspect" && !peer.closed.Load() {
				t.Fatal("timed-out inspection leaked pin")
			}
			// The socket is closed after failure, not merely left with an expired deadline.
			if _, err := right.Write([]byte{1}); err == nil {
				t.Fatal("failed authentication socket remained open")
			}
		})
	}
}
