package pirpc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartupTimeoutIndependentOfCallTimeout(t *testing.T) {
	opts := testOptions()
	opts.StartupTimeout = time.Second
	opts.CallTimeout = 20 * time.Millisecond
	c := startClient(t, "rpc_slow_state", opts)
	_, err := c.Call(callContext(t), Command{Type: "get_state"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ordinary call lost its timeout: %v", err)
	}
	requireDelivery(t, err, Uncertain)
}
func TestShutdownFaultsRemainObservable(t *testing.T) {
	for _, tc := range []struct {
		scenario string
		want     error
		events   int
	}{{"rpc_shutdown_bad", ErrProtocol, 0}, {"rpc_shutdown_overflow", ErrEventOverflow, 2}} {
		for _, mode := range []string{"close", "owner_cancel"} {
			t.Run(tc.scenario+"/"+mode, func(t *testing.T) {
				owner, cancel := context.WithCancel(context.Background())
				defer cancel()
				opts := testOptions()
				opts.MaxEvents = 2
				c, err := Start(owner, helperLaunch(t, tc.scenario), opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = c.Close(callContext(t)) })
				if mode == "close" {
					if err = c.Close(callContext(t)); !errors.Is(err, tc.want) {
						t.Errorf("close=%v", err)
					}
				} else {
					cancel()
				}
				r, err := c.Wait(callContext(t))
				if err != nil || !r.Reaped || !errors.Is(r.Cause, tc.want) {
					t.Errorf("result=%+v err=%v", r, err)
				}
				if mode == "owner_cancel" && !errors.Is(r.Cause, context.Canceled) {
					t.Errorf("first cause overwritten: %v", r.Cause)
				}
				for i := 0; i < tc.events; i++ {
					nextType(t, c, "fixture_event")
				}
				if _, err = c.NextEvent(callContext(t)); !errors.Is(err, tc.want) {
					t.Errorf("terminal event=%v", err)
				}
			})
		}
	}
}
