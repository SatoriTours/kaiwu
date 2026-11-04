package execbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

const (
	maxWireRequest  = 128 << 10
	maxWireResponse = 2 << 20
)

type wireRequest struct {
	Op                     string
	Epoch, Sequence, After uint64
	Data                   []byte
	Wait                   bool
}
type wireResponse struct {
	Epoch  uint64
	State  SessionState
	Frames []Frame
	Code   string
}
type Remote struct {
	host    *HostChannel
	epoch   uint64
	observe func(context.Context) (executionenv.Observation, error)
}

func writeWire(c net.Conn, value any, limit int) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > limit {
		return executionenv.ErrUnavailable
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(data)))
	if writeFrame(c, h[:]) != nil || writeFrame(c, data) != nil {
		return executionenv.ErrUnavailable
	}
	return nil
}
func readWire(ctx context.Context, c net.Conn, value any, limit int) error {
	var header [4]byte
	// An idle attached connection is allowed. Once any frame starts, its header
	// and body must finish within a bounded budget; a half-frame cannot live forever.
	if _, err := io.ReadFull(c, header[:1]); err != nil {
		return err
	}
	end := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(end) {
		end = d
	}
	if c.SetReadDeadline(end) != nil {
		return executionenv.ErrUnavailable
	}
	defer c.SetReadDeadline(time.Time{})
	if _, err := io.ReadFull(c, header[1:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > uint32(limit) {
		return executionenv.ErrInvalid
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(c, data); err != nil {
		return err
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if uniqueManifestValue(check, 0) != nil {
		return executionenv.ErrInvalid
	}
	if _, err := check.Token(); err != io.EOF {
		return executionenv.ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(value) != nil || dec.Decode(&struct{}{}) != io.EOF {
		return executionenv.ErrInvalid
	}
	return nil
}
func wireCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrGap):
		return "gap"
	case errors.Is(err, executionenv.ErrConflict):
		return "conflict"
	default:
		return "unavailable"
	}
}
func wireError(code string) error {
	switch code {
	case "":
		return nil
	case "gap":
		return ErrGap
	case "conflict":
		return executionenv.ErrConflict
	default:
		return executionenv.ErrUnavailable
	}
}
func OpenRemote(ctx context.Context, h *HostChannel, observe func(context.Context) (executionenv.Observation, error)) (*Remote, error) {
	if h == nil || h.closed.Load() || h.peer == nil || h.identity.Validate(true) != nil || observe == nil {
		return nil, executionenv.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h.ioMu.Lock()
	defer h.ioMu.Unlock()
	clear, err := authDeadline(ctx, h.conn)
	if err != nil {
		return nil, err
	}
	defer clear()
	var welcome wireResponse
	if readWire(ctx, h.conn, &welcome, maxWireResponse) != nil || welcome.Epoch == 0 || welcome.Code != "" {
		h.Detach()
		return nil, executionenv.ErrUnavailable
	}
	return &Remote{host: h, epoch: welcome.Epoch, observe: observe}, nil
}
func (r *Remote) Identity() executionenv.Identity { return r.host.identity }
func (r *Remote) Observe(ctx context.Context) (executionenv.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return r.observe(ctx)
}
func (r *Remote) call(ctx context.Context, request wireRequest) (wireResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r.host.ioMu.Lock()
	defer r.host.ioMu.Unlock()
	if r.host.closed.Load() {
		return wireResponse{}, executionenv.ErrUnavailable
	}
	clear, err := authDeadline(ctx, r.host.conn)
	if err != nil {
		return wireResponse{}, err
	}
	defer clear()
	var response wireResponse
	if writeWire(r.host.conn, request, maxWireRequest) != nil || readWire(ctx, r.host.conn, &response, maxWireResponse) != nil || response.Epoch != r.epoch {
		r.host.Detach()
		return wireResponse{}, executionenv.ErrUnavailable
	}
	return response, wireError(response.Code)
}
func (r *Remote) Commit(ctx context.Context, sequence uint64) error {
	response, err := r.call(ctx, wireRequest{Op: "commit", Epoch: r.epoch, Sequence: sequence})
	if err == nil && response.State.Gap {
		return ErrGap
	}
	return err
}
func (r *Remote) Input(ctx context.Context, sequence uint64, data []byte) error {
	_, err := r.call(ctx, wireRequest{Op: "input", Epoch: r.epoch, Sequence: sequence, Data: data})
	return err
}
func (r *Remote) Status(ctx context.Context) (LaunchStatus, error) {
	response, err := r.call(ctx, wireRequest{Op: "status", Epoch: r.epoch})
	return LaunchStatus{Attempted: response.State.Attempted, Executed: response.State.Executed, Gap: response.State.Gap}, err
}
func (r *Remote) Events(ctx context.Context, after uint64, wait bool) ([]Frame, error) {
	response, err := r.call(ctx, wireRequest{Op: "events", Epoch: r.epoch, After: after, Wait: wait})
	return response.Frames, err
}
func (r *Remote) Detach() error { return r.host.Detach() }

// Serve owns only this authenticated attachment, not child lifetime or the
// retained descriptor bundle. EOF/cancellation/replacement never closes stdin.
func (s *Session) Serve(ctx context.Context, c *BridgeChannel) error {
	epoch, err := s.Attach(c)
	if err != nil {
		c.Detach()
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Detach() })
	defer stop()
	defer c.Detach()
	defer func() {
		s.mu.Lock()
		if s.channel == c {
			s.channel = nil
		}
		s.mu.Unlock()
	}()
	respond := func(response wireResponse) error {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		clear, err := authDeadline(writeCtx, c.conn)
		if err != nil {
			return err
		}
		defer clear()
		return writeWire(c.conn, response, maxWireResponse)
	}
	if respond(wireResponse{Epoch: epoch, State: s.State()}) != nil {
		return executionenv.ErrUnavailable
	}
	for {
		var request wireRequest
		if err := readWire(ctx, c.conn, &request, maxWireRequest); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || c.closed.Load() {
				return nil
			}
			return executionenv.ErrUnavailable
		}
		s.mu.Lock()
		authorized := s.authorized(c, request.Epoch)
		s.mu.Unlock()
		var operationErr error
		var frames []Frame
		if !authorized {
			operationErr = executionenv.ErrConflict
		} else {
			switch request.Op {
			case "commit":
				if len(request.Data) != 0 || request.After != 0 || request.Wait {
					operationErr = executionenv.ErrConflict
				} else {
					operationErr = s.Commit(ctx, c, epoch, request.Sequence)
				}
			case "input":
				if request.After != 0 || request.Wait {
					operationErr = executionenv.ErrConflict
				} else {
					operationErr = s.Input(ctx, c, epoch, request.Sequence, request.Data)
				}
			case "status":
				if request.Sequence != 0 || request.After != 0 || len(request.Data) != 0 || request.Wait {
					operationErr = executionenv.ErrConflict
				}
			case "recover-files":
				if request.Sequence != 0 || request.After != 0 || len(request.Data) != 0 || request.Wait {
					operationErr = executionenv.ErrConflict
				} else {
					operationErr = s.sendRetained(ctx, c, epoch)
				}
			case "events":
				if request.Sequence != 0 || len(request.Data) != 0 {
					operationErr = executionenv.ErrConflict
				} else {
					readCtx, cancel := context.WithTimeout(ctx, time.Second)
					frames, operationErr = s.Events(readCtx, request.After, request.Wait)
					cancel()
				}
			default:
				operationErr = executionenv.ErrConflict
			}
		}
		if respond(wireResponse{Epoch: epoch, State: s.State(), Frames: frames, Code: wireCode(operationErr)}) != nil {
			return executionenv.ErrUnavailable
		}
	}
}
