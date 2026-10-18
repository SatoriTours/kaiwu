package pirpc

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// maxUIResponseBytes matches the product answer cap with framing headroom.
// The record is still bounded by MaxRecordBytes when configured lower.
const maxUIResponseBytes = 64 << 10

// UIResponse answers one pi extension_ui_request with its original id. Unlike
// Command it never creates a req_N waiter: pi resolves the pending dialog and
// emits no normal command response, so waiting for one would hang.
type UIResponse struct {
	Value     *string
	Confirmed *bool
	ID        string
	Cancelled bool
}

func validUpstreamID(id string) bool {
	if len(id) == 0 || len(id) > 200 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func encodeUIResponse(r UIResponse) ([]byte, error) {
	if !validUpstreamID(r.ID) {
		return nil, ErrInvalidInput
	}
	fields := 0
	if r.Value != nil {
		fields++
	}
	if r.Confirmed != nil {
		fields++
	}
	if r.Cancelled {
		fields++
	}
	// Exactly one discriminator: pi accepts value, confirmed or cancelled and
	// ignores records it cannot interpret.
	if fields != 1 {
		return nil, ErrInvalidInput
	}
	if r.Value != nil && (!utf8.ValidString(*r.Value) || len(*r.Value) > maxUIResponseBytes) {
		return nil, ErrRecordTooLarge
	}
	body := map[string]any{"type": "extension_ui_response", "id": r.ID}
	if r.Value != nil {
		body["value"] = *r.Value
	}
	if r.Confirmed != nil {
		body["confirmed"] = *r.Confirmed
	}
	if r.Cancelled {
		body["cancelled"] = true
	}
	packet, err := json.Marshal(body)
	if err != nil || len(packet)+1 > maxUIResponseBytes {
		return nil, ErrRecordTooLarge
	}
	return append(packet, '\n'), nil
}

// RespondUI writes one raw extension_ui_response through the same bounded
// single-writer queue as ordinary commands, so concurrent writes can never
// interleave records on stdin. Delivery reports the transport result only.
func (c *Client) RespondUI(ctx context.Context, r UIResponse) (Delivery, error) {
	if ctx == nil {
		return NotSent, ErrInvalidInput
	}
	if ctx.Err() != nil {
		return NotSent, contextError(ctx.Err(), NotSent)
	}
	packet, err := encodeUIResponse(r)
	if err != nil {
		return NotSent, err
	}
	bounded := ctx
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		bounded, cancel = context.WithTimeout(ctx, c.opts.CallTimeout)
		defer cancel()
	}
	c.mu.Lock()
	if c.state != ready {
		c.mu.Unlock()
		return NotSent, ErrClosed
	}
	if len(c.pending) >= c.opts.MaxPending {
		c.mu.Unlock()
		return NotSent, ErrTooManyRequests
	}
	key := "ui:" + r.ID
	if _, exists := c.pending[key]; exists {
		c.mu.Unlock()
		return NotSent, ErrInvalidInput
	}
	if len(packet)+1 > c.opts.MaxRecordBytes {
		c.mu.Unlock()
		return NotSent, ErrRecordTooLarge
	}
	bytes := len(packet) - 1
	if bytes > c.opts.MaxWriteBytes-c.writeBytes {
		c.mu.Unlock()
		return NotSent, ErrWriteQueueFull
	}
	if bounded.Err() != nil {
		c.mu.Unlock()
		return NotSent, contextError(bounded.Err(), NotSent)
	}
	c.writeBytes += bytes
	req := &pendingRequest{id: key, typ: "extension_ui_response", packet: packet, packetBytes: bytes, done: make(chan struct{})}
	c.pending[key] = req
	c.writes = append(c.writes, req)
	c.notifyWriterLocked()
	c.mu.Unlock()
	select {
	case <-req.done:
	case <-bounded.Done():
		c.mu.Lock()
		if !req.completed {
			d := Uncertain
			if req.phase == queued {
				d = NotSent
				c.removeQueuedLocked(req)
			}
			c.completeLocked(req, Response{}, contextError(bounded.Err(), d))
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	completed, failure := req.completed, req.err
	c.mu.Unlock()
	if completed && failure == nil {
		return Acknowledged, nil
	}
	if failure == nil {
		failure = ErrClosed
	}
	var protocol *Error
	if errors.As(failure, &protocol) {
		return protocol.Delivery, failure
	}
	return NotSent, failure
}
