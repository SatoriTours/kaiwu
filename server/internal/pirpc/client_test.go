package pirpc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testOptions() Options {
	return Options{StartupTimeout: 2 * time.Second, CallTimeout: 2 * time.Second, WriteTimeout: 500 * time.Millisecond, ShutdownTimeout: 100 * time.Millisecond, DrainTimeout: 500 * time.Millisecond}
}
func startClient(t *testing.T, scenario string, opts Options) *Client {
	t.Helper()
	owner, cancel := context.WithCancel(context.Background())
	c, err := Start(owner, helperLaunch(t, scenario), opts)
	if c != nil {
		t.Cleanup(func() {
			cancel()
			ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = c.Close(ctx)
			r, err := c.Wait(ctx)
			if err != nil || !r.Reaped {
				t.Errorf("cleanup: %+v %v", r, err)
				_ = c.p.cmd.Process.Kill()
			}
		})
	} else {
		t.Cleanup(cancel)
	}
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestStartHandshake(t *testing.T) {
	c := startClient(t, "rpc_event", testOptions())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ev, err := c.NextEvent(ctx)
	if err != nil || ev.Type != "future_event" {
		t.Fatalf("%+v %v", ev, err)
	}
	r, err := c.Call(ctx, Command{Type: "get_state"})
	if err != nil || !r.Success {
		t.Fatal(err)
	}
}
func TestStartRejectsInvalidState(t *testing.T) {
	for _, scenario := range []string{"rpc_bad_state", "rpc_state_type", "rpc_state_negative", "rpc_state_fraction"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := Start(ctx, helperLaunch(t, scenario), testOptions())
			if c != nil || !errors.Is(err, ErrProtocol) {
				t.Fatalf("%v %v", c, err)
			}
		})
	}
}
func TestStartFailureCleanup(t *testing.T) {
	for _, scenario := range []string{"rpc_reject_state", "rpc_no_response", "rpc_early_exit", "rpc_stdin_closed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			opts := testOptions()
			opts.StartupTimeout = 100 * time.Millisecond
			c, err := Start(ctx, helperLaunch(t, scenario), opts)
			if err == nil {
				_ = c.Close(ctx)
				t.Fatal("unexpected ready")
			}
			if c != nil {
				t.Fatal("reaped startup should return nil")
			}
		})
	}
	t.Run("canceled_before_spawn", func(t *testing.T) {
		s := helperLaunch(t, "rpc")
		marker := filepath.Join(s.Dir, "start-marker")
		s.Env = append(s.Env, "KAIWU_PI_MARKER="+marker)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c, err := Start(ctx, s, testOptions())
		if c != nil || !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("spawned canceled lifetime")
		}
	})
	t.Run("nil_context", func(t *testing.T) {
		if c, err := Start(nil, helperLaunch(t, "rpc"), testOptions()); c != nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	})
}
