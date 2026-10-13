package pirpc

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

const (
	starting = iota
	ready
	stopping
	stopped
)

type Client struct {
	mu                                           sync.Mutex
	p                                            *process
	opts                                         Options
	state                                        int
	inFlight, writeBytes                         int
	nextID                                       uint64
	pending                                      map[string]*pendingRequest
	writes                                       []*pendingRequest
	changed                                      chan struct{}
	events                                       *eventQueue
	tail                                         *tailBuffer
	stopSignal, finished, stdoutDone, stderrDone chan struct{}
	workers                                      sync.WaitGroup
	cause, cleanup                               error
	forced, eofStop                              bool
	ignored                                      uint64
}

// Start owns one direct child for ctx's lifetime and validates get_state readiness.
// On unconfirmed failed-start cleanup, it returns a stopping Client plus an error;
// retain that handle and use Wait. Readiness does not prove model/task readiness.
func Start(ctx context.Context, s LaunchSpec, opts Options) (*Client, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if ctx.Err() != nil {
		return nil, contextError(ctx.Err(), NotSent)
	}
	o, err := normalizeOptions(opts)
	if err != nil {
		return nil, err
	}
	p, err := startProcess(s)
	if err != nil {
		return nil, err
	}
	c := newClient(ctx, p, o)
	handshake, cancel := context.WithTimeout(ctx, o.StartupTimeout)
	response, err := c.call(handshake, Command{Type: "get_state"}, true)
	cancel()
	if err == nil {
		err = validateState(response.Data)
	}
	c.mu.Lock()
	if err == nil {
		if c.state != starting {
			err = ErrClosed
		} else if ctx.Err() != nil {
			err = contextError(ctx.Err(), Uncertain)
		} else {
			c.state = ready
		}
	}
	c.mu.Unlock()
	if err == nil {
		if o.Logger != nil {
			o.Logger.Info("pi rpc ready", "pid", p.pid)
		}
		return c, nil
	}
	c.beginStop(err)
	// Cleanup is independent of the failed startup context.
	waitCtx, stop := context.WithTimeout(context.Background(), cleanupBudget(o))
	defer stop()
	r, waitErr := c.Wait(waitCtx)
	if waitErr != nil {
		return c, errors.Join(err, ErrCleanup)
	}
	return nil, errors.Join(err, r.CleanupError)
}
func newClient(ctx context.Context, p *process, o Options) *Client {
	c := &Client{p: p, opts: o, state: starting, pending: make(map[string]*pendingRequest), changed: make(chan struct{}), events: newEventQueue(o.MaxEvents, o.MaxEventBytes), tail: newTailBuffer(o.StderrTailBytes), stopSignal: make(chan struct{}), finished: make(chan struct{}), stdoutDone: make(chan struct{}), stderrDone: make(chan struct{})}
	c.workers.Add(5)
	go func() { defer c.workers.Done(); defer close(c.stdoutDone); c.readStdout() }()
	go func() {
		defer c.workers.Done()
		defer close(c.stderrDone)
		_, err := io.Copy(c.tail, p.stderr)
		if err != nil && !c.isStopping() {
			c.beginStop(newError("io", NotSent, err))
		}
	}()
	go func() { defer c.workers.Done(); c.writeLoop() }()
	go func() {
		defer c.workers.Done()
		select {
		case <-ctx.Done():
			c.beginStop(contextError(ctx.Err(), NotSent))
		case <-c.stopSignal:
		}
	}()
	go func() {
		defer c.workers.Done()
		select {
		case <-p.exited:
			c.processExited()
		case <-c.stopSignal:
		}
	}()
	return c
}
func (c *Client) isStopping() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.state >= stopping }
func (c *Client) readStdout() {
	reader := newJSONLReader(c.p.stdout, c.opts.MaxRecordBytes)
	for {
		b, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.stdoutEnded()
			} else if !c.isStopping() {
				c.readFailure(err)
			} else if !errors.Is(err, os.ErrClosed) {
				c.readFailure(err)
			}
			return
		}
		r, err := decodeRecord(b)
		if err != nil {
			c.readFailure(err)
			return
		}
		if r.Event != nil {
			if err = c.events.push(*r.Event); err != nil {
				c.readFailure(err)
				return
			}
			continue
		}
		c.mu.Lock()
		req := c.pending[r.Response.ID]
		if req == nil {
			c.ignored++
			c.mu.Unlock()
			continue
		}
		if req.typ != r.Response.Command {
			c.mu.Unlock()
			c.readFailure(ErrProtocol)
			return
		}
		var reject error
		if !r.Response.Success {
			reject = newError("remote_rejected", Acknowledged, nil)
		}
		c.completeLocked(req, *r.Response, reject)
		c.mu.Unlock()
	}
}

// NextEvent is an internal single-consumer FIFO; queued events drain before cause/EOF.
func (c *Client) NextEvent(ctx context.Context) (Event, error) { return c.events.next(ctx) }

// Diagnostics explicitly opts into a copied stderr tail, which may contain secrets.
func (c *Client) Diagnostics() []byte { return c.tail.Bytes() }
func (c *Client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{IgnoredResponses: c.ignored}
}

// PID is immutable; it identifies this owned child, never an external kill target.
func (c *Client) PID() int { return c.p.pid }
