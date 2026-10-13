package pirpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
func TestClientStderrFloodAndEarlyEOF(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	for _, typ := range []string{"fixture_stderr", "fixture_stderr_close", "get_state"} {
		if _, err := c.Call(callContext(t), Command{Type: typ}); err != nil {
			t.Fatal(err)
		}
	}
	tail := c.Diagnostics()
	if len(tail) != 65536 || !bytes.HasSuffix(tail, []byte("synthetic-secret")) {
		t.Fatal("tail not bounded/consumed")
	}
}
func TestClientPrivacy(t *testing.T) {
	var logs lockedLog
	opts := testOptions()
	opts.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	c := startClient(t, "rpc", opts)
	if _, err := c.Call(callContext(t), Command{Type: "fixture_stderr"}); err != nil {
		t.Fatal(err)
	}
	r, err := c.Call(callContext(t), Command{Type: "fixture_reject", Params: json.RawMessage(`{"message":"synthetic-secret"}`)})
	if !errors.Is(err, ErrRemoteRejected) || r.ErrorText != "synthetic-secret" || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("error privacy")
	}
	if logs.String() == "" || strings.Contains(logs.String(), "synthetic-secret") {
		t.Fatalf("logger contract: %s", logs.String())
	}
}
func TestClientEventOverflow(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, bytes int
		want         int
	}{{"count", 2, 4096, 2}, {"bytes", 100, 256, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testOptions()
			opts.MaxEvents = tc.count
			opts.MaxEventBytes = tc.bytes
			c := startClient(t, "rpc", opts)
			_, err := c.Call(callContext(t), Command{Type: "fixture_events"})
			if !errors.Is(err, ErrEventOverflow) {
				t.Fatal(err)
			}
			r, err := c.Wait(callContext(t))
			if err != nil || !r.Reaped || !errors.Is(r.Cause, ErrEventOverflow) {
				t.Fatalf("%+v %v", r, err)
			}
			for i := 0; i < tc.want; i++ {
				nextType(t, c, "fixture_event")
			}
			if _, err = c.NextEvent(callContext(t)); !errors.Is(err, ErrEventOverflow) {
				t.Fatal(err)
			}
		})
	}
}
func TestClientWriteTimeout(t *testing.T) {
	opts := testOptions()
	opts.WriteTimeout = 80 * time.Millisecond
	opts.CallTimeout = 300 * time.Millisecond
	c := startClient(t, "rpc_pause", opts)
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(callContext(t), Command{Type: "fixture_big", Params: json.RawMessage(`{"text":"` + strings.Repeat("x", 1<<20) + `"}`)})
		done <- err
	}()
	nextType(t, c, "fixture_write_start")
	err := <-done
	if !errors.Is(err, ErrWriteTimeout) {
		t.Errorf("write timeout=%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.Wait(ctx)
	if err != nil || !r.Reaped {
		t.Errorf("write worker/child: %+v %v", r, err)
	}
}
func TestClientStdoutEOFAndBrokenPipe(t *testing.T) {
	for _, typ := range []string{"fixture_stdout_eof", "fixture_stdin_eof"} {
		t.Run(typ, func(t *testing.T) {
			c := startClient(t, "rpc", testOptions())
			_, err := c.Call(callContext(t), Command{Type: typ})
			if typ == "fixture_stdin_eof" {
				if err != nil {
					t.Fatal(err)
				}
				_, err = c.Call(callContext(t), Command{Type: "get_state"})
			}
			if !errors.Is(err, ErrIO) {
				t.Fatal(err)
			}
			r, err := c.Wait(callContext(t))
			if err != nil || !r.Reaped {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}
func TestClientLastResponseBeforeExit(t *testing.T) {
	for _, typ := range []string{"fixture_last0", "fixture_last7"} {
		for _, suffix := range []string{"lf", "no_lf"} {
			t.Run(typ+suffix, func(t *testing.T) {
				c := startClient(t, "rpc", testOptions())
				r, err := c.Call(callContext(t), Command{Type: typ, Params: json.RawMessage(`{"message":"` + suffix + `"}`)})
				if err != nil || !r.Success {
					t.Errorf("lost last response: %v", err)
				}
				nextType(t, c, "fixture_final")
				exit, err := c.Wait(callContext(t))
				want := 0
				if typ == "fixture_last7" {
					want = 7
				}
				if err != nil || !exit.Reaped || exit.ExitCode == nil || *exit.ExitCode != want || !errors.Is(exit.Cause, ErrProcessExit) {
					t.Errorf("exit=%+v err=%v", exit, err)
				}
			})
		}
	}
}
func TestClientCloseAndOwnerCancel(t *testing.T) {
	t.Run("normal_concurrent", func(t *testing.T) {
		c := startClient(t, "rpc", testOptions())
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := c.Close(callContext(t)); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		r, err := c.Wait(callContext(t))
		if err != nil || !r.Reaped || r.Cause != nil || r.Forced {
			t.Fatalf("%+v %v", r, err)
		}
		if _, err = c.NextEvent(callContext(t)); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	})
	t.Run("cancel_wait", func(t *testing.T) {
		c := startClient(t, "rpc_pause", testOptions())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Close(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		r, err := c.Wait(callContext(t))
		if err != nil || !r.Reaped || !r.Forced || !errors.Is(r.CleanupError, ErrShutdownTimeout) {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("owner", func(t *testing.T) {
		owner, cancel := context.WithCancel(context.Background())
		c, err := Start(owner, helperLaunch(t, "rpc"), testOptions())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(callContext(t))
		cancel()
		r, err := c.Wait(callContext(t))
		if err != nil || !r.Reaped || !errors.Is(r.Cause, context.Canceled) {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("wait_only", func(t *testing.T) {
		c := startClient(t, "rpc", testOptions())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
			t.Fatal(err)
		}
	})
}
func TestClientDrainTimeout(t *testing.T) {
	p := processForTest(t, "eof_exit")
	_ = p.closeStdin()
	awaitProcess(t, p)
	_ = p.stdout.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	p.stdout = r
	opts, _ := normalizeOptions(testOptions())
	opts.DrainTimeout = 30 * time.Millisecond
	c := newClient(context.Background(), p, opts)
	result, err := c.Wait(callContext(t))
	if err != nil || !result.Reaped || !errors.Is(result.CleanupError, ErrDrainTimeout) {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestClientRetainsHandleOnCleanupFailure(t *testing.T) {
	p := processForTest(t, "ignore_eof")
	p.kill = func() error { return os.ErrPermission }
	opts, _ := normalizeOptions(testOptions())
	opts.ShutdownTimeout = 20 * time.Millisecond
	opts.DrainTimeout = 20 * time.Millisecond
	c := newClient(context.Background(), p, opts)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	c.mu.Lock()
	state := c.state
	cleanup := c.cleanup
	c.mu.Unlock()
	if state != stopping || !errors.Is(cleanup, ErrCleanup) {
		t.Fatalf("state=%d cleanup=%v", state, cleanup)
	}
	if _, err := c.Call(callContext(t), Command{Type: "get_state"}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	_ = p.cmd.Process.Kill()
	result, err := c.Wait(callContext(t))
	if err != nil || !result.Reaped || !errors.Is(result.CleanupError, ErrCleanup) {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestClientRawOwnership(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	r, err := c.Call(callContext(t), Command{Type: "get_state"})
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), r.Raw...)
	for i := 0; i < 10; i++ {
		if _, err := c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, r.Raw) {
		t.Fatal("reused raw buffer")
	}
	_, _ = c.Call(callContext(t), Command{Type: "fixture_stderr"})
	d := c.Diagnostics()
	d[0] = 'z'
	if c.Diagnostics()[0] == 'z' {
		t.Fatal("reused diagnostic buffer")
	}
	_, _ = c.Call(callContext(t), Command{Type: "fixture_ui"})
	nextType(t, c, "extension_ui_request")
}
