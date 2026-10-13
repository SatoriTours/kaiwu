package pirpc

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEventsFIFOAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, bytes int
	}{{"count", 2, 1024}, {"bytes", 10, 256}} {
		t.Run(tc.name, func(t *testing.T) {
			q := newEventQueue(tc.count, tc.bytes)
			ev := Event{Type: "event", Raw: []byte(strings.Repeat("x", 128))}
			for _, typ := range []string{"first", "second"} {
				ev.Type = typ
				if err := q.push(ev); err != nil {
					t.Fatal(err)
				}
			}
			if err := q.push(ev); !errors.Is(err, ErrEventOverflow) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := q.next(ctx)
			if err != nil || got.Type != "first" {
				t.Fatalf("%v %v", got, err)
			}
			if err := q.push(Event{Type: "third", Raw: ev.Raw}); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"second", "third"} {
				got, err = q.next(ctx)
				if err != nil || got.Type != want {
					t.Fatalf("%v %v", got, err)
				}
			}
		})
	}
}
func TestEventsCancelDoesNotFinish(t *testing.T) {
	q := newEventQueue(2, 256)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := q.push(Event{Type: "alive"}); err != nil {
		t.Fatal(err)
	}
	ev, err := q.next(context.Background())
	if err != nil || ev.Type != "alive" {
		t.Fatal(err)
	}
}
func TestEventsDrainAfterFinish(t *testing.T) {
	for _, cause := range []error{nil, ErrProtocol} {
		q := newEventQueue(2, 256)
		_ = q.push(Event{Type: "last"})
		q.finish(cause)
		q.finish(ErrIO)
		ev, err := q.next(context.Background())
		if err != nil || ev.Type != "last" {
			t.Fatalf("%+v %v", ev, err)
		}
		_, err = q.next(context.Background())
		want := cause
		if want == nil {
			want = io.EOF
		}
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
}
func TestEventsNoLostWakeup(t *testing.T) {
	for i := 0; i < 100; i++ {
		q := newEventQueue(2, 256)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		done := make(chan error, 1)
		go func() { _, err := q.next(ctx); done <- err }()
		if i%2 == 0 {
			_ = q.push(Event{Type: "wake"})
		} else {
			q.finish(nil)
		}
		err := <-done
		cancel()
		if i%2 == 0 && err != nil || i%2 != 0 && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
}
