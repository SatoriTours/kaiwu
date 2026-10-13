package pirpc

import (
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"time"
)

const (
	queued = iota
	writing
	written
)

type pendingRequest struct {
	id, typ     string
	packet      []byte
	packetBytes int
	phase       int
	done        chan struct{}
	completed   bool
	response    Response
	err         error
}

func contextError(err error, d Delivery) error {
	code := ErrorCode("canceled")
	if errors.Is(err, context.DeadlineExceeded) {
		code = "deadline"
	}
	return newError(code, d, err)
}

// Call accepts ordinary request/response commands, not extension_ui_response.
// Cancellation never aborts/restarts the Agent or retries uncertain delivery.
func (c *Client) Call(ctx context.Context, cmd Command) (Response, error) {
	return c.call(ctx, cmd, false)
}
func (c *Client) call(ctx context.Context, cmd Command, allowStarting bool) (Response, error) {
	if ctx == nil {
		return Response{}, ErrInvalidInput
	}
	if ctx.Err() != nil {
		return Response{}, contextError(ctx.Err(), NotSent)
	}
	// Start already supplies its StartupTimeout context; ordinary Call's
	// independent budget must not shorten the handshake.
	bounded := ctx
	if !allowStarting {
		var cancel context.CancelFunc
		bounded, cancel = context.WithTimeout(ctx, c.opts.CallTimeout)
		defer cancel()
	}
	c.mu.Lock()
	if c.state != ready && !(allowStarting && c.state == starting) {
		c.mu.Unlock()
		return Response{}, ErrClosed
	}
	if c.inFlight >= c.opts.MaxPending {
		c.mu.Unlock()
		return Response{}, ErrTooManyRequests
	}
	if c.nextID == math.MaxUint64 {
		c.mu.Unlock()
		return Response{}, ErrIDExhausted
	}
	c.inFlight++
	defer func() { c.mu.Lock(); c.inFlight--; c.mu.Unlock() }()
	c.nextID++
	id := "req_" + strconv.FormatUint(c.nextID, 10)
	packet, err := encodeCommand(id, cmd, c.opts.MaxRecordBytes)
	if err != nil {
		c.mu.Unlock()
		return Response{}, err
	}
	if bounded.Err() != nil {
		c.mu.Unlock()
		return Response{}, contextError(bounded.Err(), NotSent)
	}
	bytes := len(packet) - 1
	if bytes > c.opts.MaxWriteBytes-c.writeBytes {
		c.mu.Unlock()
		return Response{}, ErrWriteQueueFull
	}
	c.writeBytes += bytes
	req := &pendingRequest{id: id, typ: cmd.Type, packet: packet, packetBytes: bytes, done: make(chan struct{})}
	c.pending[id] = req
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
	response, err := req.response, req.err
	c.mu.Unlock()
	return response, err
}
func (c *Client) notifyWriterLocked() { close(c.changed); c.changed = make(chan struct{}) }
func (c *Client) removeQueuedLocked(req *pendingRequest) {
	for i, r := range c.writes {
		if r == req {
			copy(c.writes[i:], c.writes[i+1:])
			c.writes[len(c.writes)-1] = nil
			c.writes = c.writes[:len(c.writes)-1]
			break
		}
	}
	c.releasePacketLocked(req)
}
func (c *Client) releasePacketLocked(req *pendingRequest) {
	c.writeBytes -= req.packetBytes
	req.packetBytes = 0
	req.packet = nil
}
func (c *Client) completeLocked(req *pendingRequest, r Response, err error) {
	if req.completed {
		return
	}
	req.completed = true
	req.response = r
	req.err = err
	delete(c.pending, req.id)
	close(req.done)
}
func (c *Client) writeLoop() {
	for {
		c.mu.Lock()
		if c.state >= stopping {
			c.mu.Unlock()
			return
		}
		if len(c.writes) == 0 {
			changed := c.changed
			c.mu.Unlock()
			select {
			case <-changed:
			case <-c.stopSignal:
				return
			}
			continue
		}
		req := c.writes[0]
		c.writes[0] = nil
		c.writes = c.writes[1:]
		req.phase = writing
		packet := req.packet
		c.mu.Unlock()
		callbackDone := make(chan struct{})
		watchdog := time.AfterFunc(c.opts.WriteTimeout, func() {
			defer close(callbackDone)
			c.mu.Lock()
			expired := req.phase == writing && c.state < stopping
			c.mu.Unlock()
			if expired {
				c.beginStop(ErrWriteTimeout)
				_ = c.p.closeStdin()
			}
		})
		n, err := c.p.stdin.Write(packet)
		if err == nil && n != len(packet) {
			err = io.ErrShortWrite
		}
		c.mu.Lock()
		req.phase = written
		c.releasePacketLocked(req)
		stoppingNow := c.state >= stopping
		c.mu.Unlock()
		if !watchdog.Stop() {
			<-callbackDone
		}
		if err != nil {
			if !stoppingNow {
				c.beginStop(newError("io", Uncertain, err))
			}
			return
		}
	}
}
