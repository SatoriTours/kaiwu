package pirpc

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// An exited Wait goroutine is not a reaping proof when Wait returned no
// ProcessState. Inject that OS-failure publication without creating a child.
func TestClientWaitFailureDoesNotPublishStopped(t *testing.T) {
	var files []*os.File
	pipe := func() (*os.File, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, r, w)
		return r, w
	}
	_, stdin := pipe()
	stdout, _ := pipe()
	stderr, _ := pipe()
	t.Cleanup(func() {
		for _, f := range files {
			_ = f.Close()
		}
	})
	p := &process{pid: 42, stdin: stdin, stdout: stdout, stderr: stderr, exited: make(chan struct{}), exit: processExit{Err: os.ErrInvalid}, kill: func() error { return os.ErrPermission }}
	opts, _ := normalizeOptions(testOptions())
	c := newClient(context.Background(), p, opts)
	req := &pendingRequest{id: "fixture", phase: written, done: make(chan struct{})}
	c.mu.Lock()
	c.pending[req.id] = req
	c.mu.Unlock()
	close(p.exited)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result, err := c.Wait(ctx)
	c.mu.Lock()
	state := c.state
	cleanup := c.cleanup
	c.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) || state != stopping || !errors.Is(cleanup, ErrCleanup) {
		t.Fatalf("wait=%v state=%d cleanup=%v result=%+v", err, state, cleanup, result)
	}
	if result.PID != 42 || result.Reaped || !errors.Is(result.CleanupError, ErrCleanup) {
		t.Fatalf("unconfirmed cleanup not inspectable: %+v", result)
	}
	select {
	case <-req.done:
		if !errors.Is(req.err, ErrCleanup) {
			t.Fatal(req.err)
		}
	default:
		t.Fatal("pending retained after output was closed")
	}
}
