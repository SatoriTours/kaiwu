package pirpc

import (
	"context"
	"io"
	"sync"
)

type eventNode struct {
	ev   Event
	next *eventNode
}
type eventQueue struct {
	mu                               sync.Mutex
	head, tail                       *eventNode
	count, bytes, maxCount, maxBytes int
	changed                          chan struct{}
	finished                         bool
	cause                            error
}

func newEventQueue(maxCount, maxBytes int) *eventQueue {
	return &eventQueue{maxCount: maxCount, maxBytes: maxBytes, changed: make(chan struct{})}
}
func (q *eventQueue) notify() { close(q.changed); q.changed = make(chan struct{}) }
func (q *eventQueue) push(ev Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.finished {
		return ErrClosed
	}
	if q.count >= q.maxCount || len(ev.Raw) > q.maxBytes-q.bytes {
		return ErrEventOverflow
	}
	n := &eventNode{ev: ev}
	if q.tail == nil {
		q.head = n
	} else {
		q.tail.next = n
	}
	q.tail = n
	q.count++
	q.bytes += len(ev.Raw)
	q.notify()
	return nil
}
func (q *eventQueue) next(ctx context.Context) (Event, error) {
	if ctx == nil {
		return Event{}, ErrInvalidInput
	}
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		q.mu.Lock()
		if n := q.head; n != nil {
			q.head = n.next
			if q.head == nil {
				q.tail = nil
			}
			q.count--
			q.bytes -= len(n.ev.Raw)
			ev := n.ev
			q.mu.Unlock()
			return ev, nil
		}
		if q.finished {
			err := q.cause
			q.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return Event{}, err
		}
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-changed:
		}
	}
}
func (q *eventQueue) finish(cause error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.finished {
		q.finished = true
		q.cause = cause
		q.notify()
	}
}
