// Package execbridge implements managed unit control and attested child lifetime.
// Protocol success is not evidence that an OS sandbox has been established;
// production launch still requires a trusted native attestor and controller.
package execbridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

var ErrAuth = errors.New("bridge authentication rejected")
var authMagic = []byte("KWAUTH01")

type Bootstrap struct {
	Identity      executionenv.Identity
	SpecHash      string
	HostPublicKey ed25519.PublicKey
}

// PinnedPeer must hold a kernel pidfd across both Inspect calls. The future
// native adapter must pin SO_PEERPIDFD, check host-view SO_PEERCRED/MainPID and
// double-read BootID/InvocationID/cgroup. No production fallback exists here.
// Inspect must honor ctx, including when performing external observations.
type PinnedPeer interface {
	Inspect(context.Context) (executionenv.Identity, error)
	Close() error
}

// Pin must honor ctx; a socket deadline cannot bound external inspection.
type PeerVerifier interface {
	Pin(context.Context, net.Conn) (PinnedPeer, error)
}
type HostChannel struct {
	closed   atomic.Bool
	ioMu     sync.Mutex
	conn     net.Conn
	identity executionenv.Identity
	peer     PinnedPeer
	once     sync.Once
	closeErr error
}
type BridgeChannel struct {
	closed    atomic.Bool
	ioMu      sync.Mutex
	conn      net.Conn
	bootstrap Bootstrap
	once      sync.Once
	closeErr  error
}

func (c *HostChannel) Identity() executionenv.Identity { return c.identity }

// Detach closes this authenticated control connection and pin, not a unit,
// child stdin, writer lease, process group, or any other caller's resources.
func (c *HostChannel) Detach() error {
	c.closed.Store(true)
	c.once.Do(func() {
		c.closeErr = c.conn.Close()
		if errors.Is(c.closeErr, net.ErrClosed) {
			c.closeErr = nil
		}
		c.closeErr = errors.Join(c.closeErr, c.peer.Close())
	})
	return c.closeErr
}
func (c *BridgeChannel) Detach() error {
	c.closed.Store(true)
	c.once.Do(func() {
		c.closeErr = c.conn.Close()
		if errors.Is(c.closeErr, net.ErrClosed) {
			c.closeErr = nil
		}
	})
	return c.closeErr
}
func validBootstrap(b Bootstrap) bool {
	hash, err := hex.DecodeString(b.SpecHash)
	return b.Identity.Validate(false) == nil && b.Identity.InvocationID == "" && b.Identity.ControlGroup == "" && err == nil && len(hash) == 32 && strings.ToLower(b.SpecHash) == b.SpecHash && len(b.HostPublicKey) == ed25519.PublicKeySize
}
func nonzero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}
func challengeTranscript(b Bootstrap, h, p []byte) ([]byte, error) {
	if !validBootstrap(b) || len(h) != 32 || len(p) != 32 || !nonzero(h) || !nonzero(p) || bytes.Equal(h, p) {
		return nil, ErrAuth
	}
	// Fixed-size fields plus a version/domain prefix avoid concatenation ambiguity
	// and cross-protocol signature reuse. Both parties contribute a fresh nonce.
	out := []byte("kaiwu/bridge/host-auth/v1\x00")
	out = append(out, []byte(b.Identity.RunID)...)
	out = append(out, []byte(b.Identity.BootID)...)
	out = append(out, []byte(b.SpecHash)...)
	out = append(out, h...)
	out = append(out, p...)
	return out, nil
}
func SignChallenge(key ed25519.PrivateKey, b Bootstrap, h, p []byte) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize || !bytes.Equal(key.Public().(ed25519.PublicKey), b.HostPublicKey) {
		return nil, ErrAuth
	}
	msg, err := challengeTranscript(b, h, p)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, msg), nil
}
func VerifyChallenge(b Bootstrap, h, p, sig []byte) error {
	msg, err := challengeTranscript(b, h, p)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(b.HostPublicKey, msg, sig) {
		return ErrAuth
	}
	return nil
}
func authDeadline(ctx context.Context, c net.Conn) (func(), error) {
	if ctx.Err() != nil {
		return nil, ErrAuth
	}
	end := time.Now().Add(5 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(end) {
		end = deadline
	}
	if c.SetDeadline(end) != nil {
		return nil, ErrAuth
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.SetDeadline(time.Now()); close(done) })
	return func() {
		if !stop() {
			<-done
		}
		c.SetDeadline(time.Time{})
	}, nil
}
func writeFrame(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func peerMatches(b Bootstrap, i executionenv.Identity) bool {
	return i.Validate(true) == nil && i.RunID == b.Identity.RunID && i.BootID == b.Identity.BootID && i.Unit == b.Identity.Unit
}
func AuthenticateHost(ctx context.Context, c net.Conn, b Bootstrap, key ed25519.PrivateKey, v PeerVerifier) (out *HostChannel, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c == nil {
		return nil, ErrAuth
	}
	defer func() {
		if out == nil {
			c.Close()
		}
	}()
	if !validBootstrap(b) || v == nil || len(key) != ed25519.PrivateKeySize || !bytes.Equal(key.Public().(ed25519.PublicKey), b.HostPublicKey) {
		return nil, ErrAuth
	}
	clear, err := authDeadline(ctx, c)
	if err != nil {
		return nil, ErrAuth
	}
	defer clear()
	peer, err := v.Pin(ctx, c)
	if err != nil || peer == nil {
		if peer != nil {
			peer.Close()
		}
		return nil, ErrAuth
	}
	defer func() {
		if out == nil {
			peer.Close()
		}
	}()
	first, err := peer.Inspect(ctx)
	if err != nil || !peerMatches(b, first) {
		return nil, ErrAuth
	}
	hello := make([]byte, 40)
	if _, err := io.ReadFull(c, hello); err != nil || !bytes.Equal(hello[:8], authMagic) {
		return nil, ErrAuth
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return nil, ErrAuth
	}
	sig, err := SignChallenge(key, b, nonce, hello[8:])
	if err != nil {
		return nil, ErrAuth
	}
	reply := append(append([]byte{}, nonce...), sig...)
	if writeFrame(c, reply) != nil {
		return nil, ErrAuth
	}
	ack := make([]byte, 40)
	if _, err = io.ReadFull(c, ack); err != nil || !bytes.Equal(ack[:8], authMagic) || !bytes.Equal(ack[8:], nonce) {
		return nil, ErrAuth
	}
	second, err := peer.Inspect(ctx)
	if err != nil || second != first || ctx.Err() != nil {
		return nil, ErrAuth
	}
	return &HostChannel{conn: c, identity: first, peer: peer}, nil
}
func AuthenticateBridge(ctx context.Context, c net.Conn, b Bootstrap) (out *BridgeChannel, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c == nil {
		return nil, ErrAuth
	}
	defer func() {
		if out == nil {
			c.Close()
		}
	}()
	if !validBootstrap(b) {
		return nil, ErrAuth
	}
	b.HostPublicKey = append(ed25519.PublicKey(nil), b.HostPublicKey...)
	clear, err := authDeadline(ctx, c)
	if err != nil {
		return nil, ErrAuth
	}
	defer clear()
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil || !nonzero(nonce) {
		return nil, ErrAuth
	}
	hello := append(append([]byte{}, authMagic...), nonce...)
	if writeFrame(c, hello) != nil {
		return nil, ErrAuth
	}
	reply := make([]byte, 96)
	if _, err = io.ReadFull(c, reply); err != nil || VerifyChallenge(b, reply[:32], nonce, reply[32:]) != nil {
		return nil, ErrAuth
	}
	ack := append(append([]byte{}, authMagic...), reply[:32]...)
	if writeFrame(c, ack) != nil || ctx.Err() != nil {
		return nil, ErrAuth
	}
	return &BridgeChannel{conn: c, bootstrap: b}, nil
}
