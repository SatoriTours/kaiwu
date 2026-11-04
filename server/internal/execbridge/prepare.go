package execbridge

import (
	"context"
	"os"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type prepareOffer struct {
	Version  int
	Identity executionenv.Identity
	Bundle   BundleExpectation
}
type prepareReceipt struct {
	Version  int
	State    string
	Identity executionenv.Identity
}

// Prepare transfers only after the authenticated bridge accepts the complete
// identity/FD declaration. Success means attested, not executed. The caller must
// durably record Prepared before CommitPrepared; this never sends EXEC_COMMIT.
// It owns the initial channel exchange exclusively, before OpenRemote/Serve.
func (h *HostChannel) Prepare(parent context.Context, files []*os.File, w BundleExpectation) (err error) {
	if h == nil {
		return executionenv.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	h.ioMu.Lock()
	defer h.ioMu.Unlock()
	defer func() {
		if err != nil {
			h.Detach()
		}
	}()
	if h.closed.Load() || h.peer == nil || h.identity.Validate(true) != nil || validateBundle(files, w) != nil {
		return executionenv.ErrUnavailable
	}
	clear, err := authDeadline(ctx, h.conn)
	if err != nil {
		return executionenv.ErrUnavailable
	}
	defer clear()
	offer := prepareOffer{Version: 1, Identity: h.identity, Bundle: w}
	if writeWire(h.conn, offer, 16384) != nil {
		return executionenv.ErrUnavailable
	}
	var accepted prepareReceipt
	if readWire(ctx, h.conn, &accepted, 16384) != nil || accepted != (prepareReceipt{Version: 1, State: "accept-files", Identity: h.identity}) {
		return executionenv.ErrUnavailable
	}
	if h.sendBundleLocked(ctx, files, w) != nil {
		return executionenv.ErrUnavailable
	}
	// The SCM helper clears its temporary deadline; reinstate the shared budget
	// before reading PREPARED. No stage receives a new five-second allowance.
	reset, err := authDeadline(ctx, h.conn)
	if err != nil {
		return executionenv.ErrUnavailable
	}
	defer reset()
	var ready prepareReceipt
	if readWire(ctx, h.conn, &ready, 16384) != nil || ready != (prepareReceipt{Version: 1, State: "prepared", Identity: h.identity}) || ctx.Err() != nil {
		return executionenv.ErrUnavailable
	}
	return nil
}

// AcceptPrepare requires a trusted attestor, never one supplied in the offer.
// Once a Session exists its retained FDs belong to that Session, EVEN when the
// PREPARED receipt is lost: a non-nil Session plus error must be retained for
// query-only reattach or exact whole-group-gone cleanup, not silently closed.
// No task is executed here. It precedes Session.Serve on this connection.
func (b *BridgeChannel) AcceptPrepare(parent context.Context, a Attestor) (session *Session, err error) {
	if b == nil {
		return nil, executionenv.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	b.ioMu.Lock()
	defer b.ioMu.Unlock()
	defer func() {
		if err != nil {
			b.Detach()
		}
	}()
	if b.closed.Load() || a == nil || !validBootstrap(b.bootstrap) {
		return nil, executionenv.ErrUnavailable
	}
	clear, err := authDeadline(ctx, b.conn)
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	defer clear()
	var offer prepareOffer
	if readWire(ctx, b.conn, &offer, 16384) != nil || offer.Version != 1 || offer.Identity.Validate(true) != nil || !peerMatches(b.bootstrap, offer.Identity) || !validBundleDeclaration(offer.Bundle) {
		return nil, executionenv.ErrUnavailable
	}
	if writeWire(b.conn, prepareReceipt{Version: 1, State: "accept-files", Identity: offer.Identity}, 16384) != nil {
		return nil, executionenv.ErrUnavailable
	}
	bundle, err := b.receiveBundleLocked(ctx, offer.Bundle)
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	session, err = PrepareSession(ctx, b, offer.Identity, bundle, offer.Bundle, a)
	if err != nil {
		return nil, err
	}
	reset, err := authDeadline(ctx, b.conn)
	if err != nil {
		return session, executionenv.ErrUnavailable
	}
	defer reset()
	if writeWire(b.conn, prepareReceipt{Version: 1, State: "prepared", Identity: offer.Identity}, 16384) != nil || ctx.Err() != nil {
		return session, executionenv.ErrUnavailable
	}
	return session, nil
}
func validBundleDeclaration(w BundleExpectation) bool {
	if w.Config.Inode == 0 || w.Spool.Inode == 0 || w.Config == w.Spool {
		return false
	}
	if !w.HasLease {
		return w.Lease == (FileIdentity{})
	}
	return w.Lease.Inode != 0 && w.Lease != w.Config && w.Lease != w.Spool
}
