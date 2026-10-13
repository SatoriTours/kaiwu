package pirpc

import (
	"context"
	"errors"
	"time"
)

func cleanupBudget(o Options) time.Duration {
	max := time.Duration(1<<63 - 1)
	if o.DrainTimeout > max/2 || o.ShutdownTimeout > max-2*o.DrainTimeout {
		return max
	}
	return o.ShutdownTimeout + 2*o.DrainTimeout
}
func (c *Client) beginStop(cause error) { c.beginStopMode(cause, false, false) }
func (c *Client) processExited()        { c.beginStopMode(ErrProcessExit, true, false) }
func (c *Client) stdoutEnded()          { c.beginStopMode(ErrIO, false, true) }
func (c *Client) beginStopMode(cause error, drain, eof bool) {
	c.mu.Lock()
	if c.state >= stopping {
		c.mu.Unlock()
		return
	}
	c.state = stopping
	c.cause = cause
	c.eofStop = eof
	close(c.stopSignal)
	c.notifyWriterLocked()
	for _, r := range c.pending {
		if !drain || r.phase == queued {
			c.failRequestLocked(r, cause)
		}
	}
	c.writes = nil
	c.mu.Unlock()
	go c.cleanupProcess()
}
func (c *Client) failRequestLocked(r *pendingRequest, cause error) {
	d := Uncertain
	if r.phase == queued {
		d = NotSent
	}
	err := cause
	if err == nil {
		err = ErrClosed
	}
	var re *Error
	if errors.As(err, &re) {
		err = newError(re.Code, d, err)
	}
	c.completeLocked(r, Response{}, err)
	if r.phase == queued {
		c.releasePacketLocked(r)
	}
}
func (c *Client) readFailure(err error) {
	c.mu.Lock()
	if c.state >= stopping {
		// Shutdown output is still part of the protocol. The reader filters
		// our own expected pipe closure, not real protocol/overflow failures.
		c.cause = errors.Join(c.cause, err)
		for _, r := range c.pending {
			c.failRequestLocked(r, err)
		}
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.beginStop(err)
}
func (c *Client) cleanupProcess() {
	result, forced, err := c.p.stop(c.opts.ShutdownTimeout, c.opts.DrainTimeout)
	c.mu.Lock()
	c.forced = forced
	c.cleanup = err
	if c.eofStop && !forced && result.Reaped {
		c.cause = errors.Join(c.cause, ErrProcessExit)
	}
	c.mu.Unlock()
	if !result.Reaped {
		// No further response can be delivered after closing the outputs.
		c.mu.Lock()
		for _, r := range c.pending {
			c.failRequestLocked(r, ErrCleanup)
		}
		c.mu.Unlock()
		closeErr := c.p.closeReaders()
		if closeErr != nil {
			c.mu.Lock()
			c.cleanup = errors.Join(c.cleanup, newError("cleanup", NotSent, closeErr))
			c.mu.Unlock()
		}
		<-c.p.exited
		if !c.p.result().Reaped {
			// A completed Wait without ProcessState does not prove reaping.
			// Retain the handle/state, but don't retain finished I/O workers.
			c.workers.Wait()
			c.mu.Lock()
			cause := errors.Join(c.cause, c.cleanup)
			c.mu.Unlock()
			c.events.finish(cause)
			return
		}
	}
	timer := time.NewTimer(c.opts.DrainTimeout)
	for _, done := range []<-chan struct{}{c.stdoutDone, c.stderrDone} {
		select {
		case <-done:
		case <-timer.C:
			c.mu.Lock()
			c.cleanup = errors.Join(c.cleanup, ErrDrainTimeout)
			c.mu.Unlock()
			_ = c.p.closeReaders()
			goto drained
		}
	}
drained:
	timer.Stop()
	if err := c.p.closeReaders(); err != nil {
		c.mu.Lock()
		c.cleanup = errors.Join(c.cleanup, newError("cleanup", NotSent, err))
		c.mu.Unlock()
	}
	c.mu.Lock()
	for _, r := range c.pending {
		c.failRequestLocked(r, c.cause)
	}
	c.mu.Unlock()
	c.workers.Wait()
	c.mu.Lock()
	c.state = stopped
	cause := errors.Join(c.cause, c.cleanup)
	forced = c.forced
	c.mu.Unlock()
	c.events.finish(cause)
	if c.opts.Logger != nil {
		c.opts.Logger.Info("pi rpc stopped", "pid", c.p.pid, "forced", forced, "reaped", c.p.result().Reaped)
	}
	close(c.finished)
}

// Close begins idempotent direct-child cleanup; ctx bounds only this waiter.
// It does not manage descendants, nor does disconnecting a caller close Client.
func (c *Client) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	c.beginStop(nil)
	r, err := c.Wait(ctx)
	if err != nil {
		return err
	}
	return errors.Join(r.Cause, r.CleanupError)
}

// Wait returns a copied transport result, never task success. A nil error means
// cleanup completed; otherwise the partial snapshot is still inspectable.
// Unconfirmed reaping leaves Client stopping and requires a bounded waiter.
func (c *Client) Wait(ctx context.Context) (ExitResult, error) {
	if ctx == nil {
		return ExitResult{}, ErrInvalidInput
	}
	select {
	case <-c.finished:
	default:
		select {
		case <-c.finished:
		case <-ctx.Done():
			return c.exitSnapshot(), ctx.Err()
		}
	}
	return c.exitSnapshot(), nil
}
func (c *Client) exitSnapshot() ExitResult {
	var r processExit
	select {
	case <-c.p.exited:
		r = c.p.result()
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return ExitResult{PID: c.p.pid, ExitCode: r.ExitCode, Reaped: r.Reaped, Forced: c.forced, Cause: c.cause, CleanupError: c.cleanup}
}
