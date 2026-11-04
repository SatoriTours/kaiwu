package execbridge

import (
	"context"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func (r *Remote) RecoverFiles(ctx context.Context, w BundleExpectation) (out *Bundle, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r.host.ioMu.Lock()
	defer r.host.ioMu.Unlock()
	if r.host.closed.Load() {
		return nil, executionenv.ErrUnavailable
	}
	clear, err := authDeadline(ctx, r.host.conn)
	if err != nil {
		return nil, err
	}
	defer clear()
	if writeWire(r.host.conn, wireRequest{Op: "recover-files", Epoch: r.epoch}, maxWireRequest) != nil {
		r.host.Detach()
		return nil, executionenv.ErrUnavailable
	}
	bundle, err := receiveRetained(ctx, r.host, w)
	if err != nil {
		r.host.Detach()
		return nil, err
	}
	defer func() {
		if out == nil {
			bundle.Close()
		}
	}()
	var response wireResponse
	if readWire(ctx, r.host.conn, &response, maxWireResponse) != nil || response.Epoch != r.epoch || response.Code != "" {
		r.host.Detach()
		return nil, executionenv.ErrUnavailable
	}
	return bundle, nil
}
func (s *Session) sendRetained(ctx context.Context, c *BridgeChannel, epoch uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorized(c, epoch) {
		return executionenv.ErrConflict
	}
	return sendRetained(ctx, c, s.bundle.Files[1:])
}
