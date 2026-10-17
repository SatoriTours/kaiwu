package workbench

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncUsesTextDeltaAndFallsBackForMissingHistory(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Update(func(st *State) error {
		st.Conversations["c"] = Conversation{ID: "c", Messages: []Message{{ID: "m", Role: "assistant", Text: strings.Repeat("a", 10000)}}}
		return nil
	})
	before := s.Snapshot().Revision
	s.Update(func(st *State) error {
		c := st.Conversations["c"]
		c.Messages[0].Text += "tail"
		st.Conversations["c"] = c
		return nil
	})
	events, reset := s.SyncSince(before)
	if reset != nil || len(events) != 1 {
		t.Fatal(events, reset)
	}
	b, _ := json.Marshal(events)
	if len(b) > 1000 || !strings.Contains(string(b), `"append"`) {
		t.Fatal("full history retransmitted", len(b))
	}
	_, reset = s.SyncSince(s.Snapshot().Revision + 99)
	if reset == nil {
		t.Fatal("bad cursor did not reset")
	}
	for i := 0; i < 260; i++ {
		s.Update(func(st *State) error { return nil })
	}
	_, reset = s.SyncSince(before)
	if reset == nil {
		t.Fatal("history gap not detected")
	}
}
