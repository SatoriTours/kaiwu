package pirpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func callContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func requireDelivery(t *testing.T, err error, want Delivery) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Delivery != want {
		t.Fatalf("error=%v delivery want=%v", err, want)
	}
}
func nextType(t *testing.T, c *Client, want string) {
	t.Helper()
	ev, err := c.NextEvent(callContext(t))
	if err != nil || ev.Type != want {
		t.Fatalf("event=%+v err=%v want=%s", ev, err, want)
	}
}
func TestCallOutOfOrderAndInterleavedEvents(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	ctx := callContext(t)
	var wg sync.WaitGroup
	ids := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := c.Call(ctx, Command{Type: "fixture_order", Params: json.RawMessage(fmt.Sprintf(`{"seq":%d}`, i))})
			var data struct{ Seq int }
			if err != nil || json.Unmarshal(r.Data, &data) != nil || data.Seq != i {
				t.Errorf("seq=%d response=%+v err=%v", i, r, err)
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if id == "" || seen[id] {
			t.Fatal("nonunique ID")
		}
		seen[id] = true
	}
	for i := 0; i < 32; i++ {
		nextType(t, c, "future_event")
	}
}
func TestCallRemoteRejected(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	r, err := c.Call(callContext(t), Command{Type: "fixture_reject"})
	if r.Success || r.ErrorText != "synthetic-secret" || !errors.Is(err, ErrRemoteRejected) {
		t.Fatalf("%+v %v", r, err)
	}
	requireDelivery(t, err, Acknowledged)
	if _, err = c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
		t.Fatal(err)
	}
}
func TestCallLateUnknownDuplicateResponses(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	if _, err := c.Call(callContext(t), Command{Type: "fixture_duplicates"}); err != nil {
		t.Fatal(err)
	}
	nextType(t, c, "fixture_done")
	if c.Stats().IgnoredResponses != 2 {
		t.Fatal(c.Stats())
	}
}
func TestCallBadResponseStopsTransport(t *testing.T) {
	for _, typ := range []string{"fixture_bad_command", "fixture_no_id", "fixture_no_success"} {
		t.Run(typ, func(t *testing.T) {
			c := startClient(t, "rpc", testOptions())
			if _, err := c.Call(callContext(t), Command{Type: typ}); !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
			r, err := c.Wait(callContext(t))
			if err != nil || !r.Reaped || !errors.Is(r.Cause, ErrProtocol) {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}
func TestCallDispositionIsAcceptanceOnly(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	for _, d := range []string{"started", "queued", "handled"} {
		r, err := c.Call(callContext(t), Command{Type: "prompt", Params: json.RawMessage(`{"message":"` + d + `"}`)})
		if err != nil || string(r.Data) != `{"disposition":"`+d+`"}` {
			t.Fatalf("%+v %v", r, err)
		}
	}
}
func TestAgentEventsDoNotCompleteTask(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	if _, err := c.Call(callContext(t), Command{Type: "fixture_agent_events"}); err != nil {
		t.Fatal(err)
	}
	nextType(t, c, "agent_end")
	nextType(t, c, "agent_settled")
	if _, err := c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
		t.Fatal(err)
	}
}
func TestCallQueuedCancellationReleasesBudget(t *testing.T) {
	opts := testOptions()
	opts.WriteTimeout = 3 * time.Second
	opts.MaxPending = 2
	opts.MaxWriteBytes = (1 << 20) + 256
	c := startClient(t, "rpc_pause", opts)
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(callContext(t), Command{Type: "fixture_big", Params: json.RawMessage(`{"text":"` + strings.Repeat("x", 1<<20) + `"}`)})
		done <- err
	}()
	nextType(t, c, "fixture_write_start")
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_, err := c.Call(ctx, Command{Type: "fixture_echo"})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		requireDelivery(t, err, NotSent)
		c.mu.Lock()
		n := len(c.writes)
		c.mu.Unlock()
		if n != 0 {
			t.Fatal("canceled queue retained")
		}
	}
	_ = c.Close(callContext(t))
	err := <-done
	requireDelivery(t, err, Uncertain)
}
func TestCallSentCancellationIsUncertain(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Call(ctx, Command{Type: "fixture_late"}); done <- err }()
	nextType(t, c, "fixture_received")
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	requireDelivery(t, err, Uncertain)
	if _, err = c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().IgnoredResponses != 1 {
		t.Fatal(c.Stats())
	}
}
func TestCallConfirmedResponseWinsCancellation(t *testing.T) {
	c := startClient(t, "rpc", testOptions())
	c.mu.Lock()
	r := &pendingRequest{id: "manual", done: make(chan struct{})}
	c.pending[r.id] = r
	c.completeLocked(r, Response{ID: r.id, Success: true}, nil)
	c.completeLocked(r, Response{}, context.Canceled)
	if !r.response.Success || r.err != nil {
		t.Fatal("overwrote confirmed response")
	}
	c.mu.Unlock()
}
func TestCallLimitsAndIDExhaustion(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		opts := testOptions()
		opts.MaxPending = 1
		c := startClient(t, "rpc", opts)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := c.Call(ctx, Command{Type: "fixture_hold"}); done <- err }()
		nextType(t, c, "fixture_received")
		_, err := c.Call(callContext(t), Command{Type: "get_state"})
		if !errors.Is(err, ErrTooManyRequests) {
			t.Errorf("capacity err=%v", err)
		}
		cancel()
		<-done
		if _, err = c.Call(callContext(t), Command{Type: "get_state"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		opts := testOptions()
		opts.MaxWriteBytes = 256
		c := startClient(t, "rpc", opts)
		_, err := c.Call(callContext(t), Command{Type: "fixture_big", Params: json.RawMessage(`{"text":"` + strings.Repeat("x", 512) + `"}`)})
		if !errors.Is(err, ErrWriteQueueFull) {
			t.Fatal(err)
		}
	})
	t.Run("id", func(t *testing.T) {
		c := startClient(t, "rpc", testOptions())
		c.mu.Lock()
		c.nextID = math.MaxUint64
		c.mu.Unlock()
		_, err := c.Call(callContext(t), Command{Type: "get_state"})
		if !errors.Is(err, ErrIDExhausted) {
			t.Fatal(err)
		}
		requireDelivery(t, err, NotSent)
	})
}
