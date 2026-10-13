package workbench

import (
	"path/filepath"
	"testing"
)

func TestReviewTaskConversationMismatch(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cmd := func(c Command) Receipt {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
	a := cmd(Command{OperationID: "task-a", Type: "add_task", ProjectID: p, Request: "a"}).CreatedID
	b := cmd(Command{OperationID: "task-b", Type: "add_task", ProjectID: p, Request: "b"}).CreatedID
	_, e = s.Command(Command{OperationID: "bad-send", Revision: s.Snapshot().Revision, Type: "send_message", ProjectID: p, TaskID: b, ConversationID: s.Snapshot().Tasks[a].ConversationID, Text: "intended for B"})
	if e == nil {
		t.Fatal("mismatched task B/conversation A accepted")
	}
}
func TestReviewFailedTransactionNotPublished(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	before := s.Snapshot()
	changes, unsub := s.Subscribe()
	defer unsub()
	<-changes
	s.db.Close()
	e = s.Update(func(st *State) error { st.Projects["unexpected"] = Project{ID: "unexpected"}; return nil })
	if e == nil {
		t.Fatal("expected transaction failure")
	}
	if len(s.Snapshot().Projects) != len(before.Projects) || s.Snapshot().Revision != before.Revision {
		t.Fatal("failed transaction published")
	}
	select {
	case <-changes:
		t.Fatal("failure notified subscribers")
	default:
	}
}

func TestRestartRequiresExplicitRestoreOfIdleSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Command(Command{OperationID: "project", Revision: s.Snapshot().Revision, Type: "add_project", Name: "p", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	id := s.Snapshot().Projects[r.CreatedID].ConversationID
	s.Update(func(st *State) error {
		c := st.Conversations[id]
		c.SessionFile = "/private/session.jsonl"
		c.Messages = []Message{{ID: "done", Role: "user", Text: "past", Delivery: "complete"}}
		st.Conversations[id] = c
		return nil
	})
	s.Close()
	s, e = Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c := s.Snapshot().Conversations[id]
	if c.RunState != "interrupted" || c.Messages[0].Delivery != "complete" {
		t.Fatalf("restart lost restore boundary/history: %+v", c)
	}
}
