package execbridge

import (
	"context"
	"crypto/ed25519"
	"net"
	"sync"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

// BridgeRuntime is the single owner of one bridge's Session, independent of
// control connection lifetime. Construct only after dedicated-process hardening
// and trusted public bootstrap loading. It does not listen, dial, spawn units,
// signal reconnects, or supply an attestor. The CLI/native caller is not wired.
type BridgeRuntime struct {
	mu                sync.Mutex
	bootstrap         Bootstrap
	attestor          Attestor
	session           *Session
	preparing, closed bool
}

func NewBridgeRuntime(b Bootstrap, a Attestor) (*BridgeRuntime, error) {
	if !validBootstrap(b) || a == nil {
		return nil, executionenv.ErrUnavailable
	}
	b.HostPublicKey = append(ed25519.PublicKey(nil), b.HostPublicKey...)
	return &BridgeRuntime{bootstrap: b, attestor: a}, nil
}

// ServeConnection owns conn, not the child lifetime. Once a Session has been
// prepared, every new authenticated connection goes directly to attach/status;
// PREPARE and old task input are never replayed. Concurrent first prepares are
// rejected, not queued for a second initialization.
func (r *BridgeRuntime) ServeConnection(ctx context.Context, conn net.Conn) error {
	if conn == nil {
		return executionenv.ErrUnavailable
	}
	defer conn.Close()
	if r == nil {
		return executionenv.ErrUnavailable
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return executionenv.ErrUnavailable
	}
	channel, err := AuthenticateBridge(ctx, conn, r.bootstrap)
	if err != nil {
		return err
	}
	defer channel.Detach()
	r.mu.Lock()
	if r.closed || r.preparing {
		r.mu.Unlock()
		return executionenv.ErrConflict
	}
	s := r.session
	if s == nil {
		r.preparing = true
	}
	r.mu.Unlock()
	if s == nil {
		prepared, prepareErr := channel.AcceptPrepare(ctx, r.attestor)
		r.mu.Lock()
		// Ownership is installed even when sending PREPARED failed. No generic
		// error/defer path closes the retained task lease or spool.
		r.session = prepared
		r.preparing = false
		r.mu.Unlock()
		if prepareErr != nil {
			return prepareErr
		}
		s = prepared
	}
	if s == nil {
		return executionenv.ErrUnavailable
	}
	return s.Serve(ctx, channel)
}

// Only a trusted native whole-group observation can release retained resources.
// There is deliberately no generic Close(context cancellation) lifecycle.
func (r *BridgeRuntime) CloseAfterGroupGone(proof executionenv.Observation) error {
	if r == nil {
		return executionenv.ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.preparing || r.session == nil {
		return executionenv.ErrConflict
	}
	if err := r.session.CloseAfterGroupGone(proof); err != nil {
		return err
	}
	r.closed = true
	return nil
}
