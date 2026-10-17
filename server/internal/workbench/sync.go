package workbench

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

type SyncOp struct {
	Op    string   `json:"op"`
	Path  []string `json:"path"`
	Value any      `json:"value"`
}
type SyncEvent struct {
	After    uint64   `json:"after"`
	Revision uint64   `json:"revision"`
	Ops      []SyncOp `json:"ops"`
}

func jsonValue(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}
func diffValue(old, next any, path []string, out *[]SyncOp) {
	if reflect.DeepEqual(old, next) {
		return
	}
	add := func(op string, p []string, v any) { *out = append(*out, SyncOp{op, append([]string{}, p...), v}) }
	a, am := old.(map[string]any)
	b, bm := next.(map[string]any)
	if am && bm {
		for k := range a {
			if _, ok := b[k]; !ok {
				add("remove", append(path, k), nil)
			}
		}
		for k, v := range b {
			if prev, ok := a[k]; ok {
				diffValue(prev, v, append(path, k), out)
			} else {
				add("add", append(path, k), v)
			}
		}
		return
	}
	x, xa := old.([]any)
	y, ya := next.([]any)
	if xa && ya && len(y) >= len(x) {
		for i, v := range y {
			p := append(path, strconv.Itoa(i))
			if i < len(x) {
				diffValue(x[i], v, p, out)
			} else {
				add("add", p, v)
			}
		}
		return
	}
	if a, ok := old.(string); ok {
		if b, ok := next.(string); ok && len(path) > 0 && path[len(path)-1] == "text" && strings.HasPrefix(b, a) && a != "" {
			add("append", path, b[len(a):])
			return
		}
	}
	add("replace", path, next)
}
func (s *Store) recordSync(before, next State) {
	event := SyncEvent{After: before.Revision, Revision: next.Revision, Ops: []SyncOp{}}
	diffValue(jsonValue(before), jsonValue(next), nil, &event.Ops)
	b, _ := json.Marshal(event)
	size := len(b)
	if size > 2<<20 {
		s.syncEvents = nil
		s.syncBytes = 0
		return
	}
	s.syncEvents = append(s.syncEvents, event)
	s.syncBytes += size
	for len(s.syncEvents) > 256 || s.syncBytes > 2<<20 {
		b, _ := json.Marshal(s.syncEvents[0])
		s.syncBytes -= len(b)
		s.syncEvents[0] = SyncEvent{}
		s.syncEvents = s.syncEvents[1:]
	}
}

// Store owns the replay window; missing history resets once from authoritative state.
func (s *Store) SyncSince(after uint64) ([]SyncEvent, *State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if after == s.state.Revision {
		return []SyncEvent{}, nil
	}
	for i, e := range s.syncEvents {
		if e.After == after {
			copyEvents := jsonValue(s.syncEvents[i:])
			b, _ := json.Marshal(copyEvents)
			var events []SyncEvent
			_ = json.Unmarshal(b, &events)
			return events, nil
		}
	}
	st := clone(s.state)
	return nil, &st
}
