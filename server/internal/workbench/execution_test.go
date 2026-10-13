package workbench

import (
	"path/filepath"
	"testing"
)

func TestStartTaskPreservesSessionAndDeduplicates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), Profile{Provider: "fixture", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cmd := func(c Command) Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
	id := cmd(Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "实现功能"}).CreatedID
	before := s.Snapshot()
	conv := before.Tasks[id].ConversationID
	c := Command{OperationID: "start", Type: "start_task", ProjectID: p, TaskID: id, ConversationID: conv}
	r, e := s.Command(c)
	if e != nil {
		t.Fatal(e)
	}
	after := s.Snapshot()
	if after.Tasks[id].Status != "running" || after.Conversations[conv].SessionID != before.Conversations[conv].SessionID || r.ConversationID != conv {
		t.Fatal("start lost card identity")
	}
	if len(after.Conversations[conv].Messages) != 1 || after.Conversations[conv].Messages[0].Text != "开始开发" {
		t.Fatal("start instruction not saved")
	}
	replay, e := s.Command(c)
	if e != nil || !replay.Replay || replay.CreatedID != r.CreatedID {
		t.Fatal("start replay duplicated")
	}
	c.OperationID = "second-click"
	if _, e = s.Command(c); e == nil {
		t.Fatal("second operation bypassed running task lock")
	}
}

func TestStartPhraseIsExplicitAndTaskScoped(t *testing.T) {
	for _, phrase := range []string{"开始开发", "  开始开发！ ", "先不要开始开发"} {
		t.Run(phrase, func(t *testing.T) {
			s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{Provider: "test", Model: "test"})
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
			p := cmd(Command{OperationID: "p-key", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
			id := cmd(Command{OperationID: "t-key", Type: "add_task", ProjectID: p, Request: "需求"}).CreatedID
			conv := s.Snapshot().Tasks[id].ConversationID
			r := cmd(Command{OperationID: "send-key", Type: "send_message", ProjectID: p, TaskID: id, ConversationID: conv, Text: phrase})
			want := "start_task"
			if phrase == "先不要开始开发" {
				want = "send_message"
			}
			if r.Type != want {
				t.Fatalf("got %s, want %s", r.Type, want)
			}
			if want == "start_task" && s.Snapshot().Tasks[id].Status != "running" {
				t.Fatal("chat did not start task")
			}
		})
	}
}

func TestStartRejectsMissingModelAndForeignConversation(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Command(Command{OperationID: "p-key", Revision: s.Snapshot().Revision, Type: "add_project", Name: "p", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Command(Command{OperationID: "t-key", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: p.CreatedID, Request: "需求"})
	if e != nil {
		t.Fatal(e)
	}
	id := r.CreatedID
	before := s.Snapshot()
	if _, e = s.Command(Command{OperationID: "start-key", Type: "start_task", ProjectID: p.CreatedID, TaskID: id}); e == nil {
		t.Fatal("unconfigured task accepted")
	}
	if s.Snapshot().Tasks[id].Status != "idle" || s.Snapshot().Revision != before.Revision {
		t.Fatal("rejected start changed state")
	}
	if e = s.Update(func(st *State) error { st.Profile = Profile{Provider: "new", Model: "new"}; return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Command(Command{OperationID: "foreign-key", Type: "start_task", ProjectID: p.CreatedID, TaskID: id, ConversationID: before.Projects[p.CreatedID].ConversationID}); e == nil {
		t.Fatal("foreign conversation accepted")
	}
	if _, e = s.Command(Command{OperationID: "start-key", Type: "start_task", ProjectID: p.CreatedID, TaskID: id}); e != nil {
		t.Fatal("unused task could not freeze new config", e)
	}
}

func TestQueuedDevelopmentRestartIsInterruptedWithoutReplay(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db")
	s, e := Open(db, Profile{Provider: "test", Model: "test"})
	if e != nil {
		t.Fatal(e)
	}
	cmd := func(c Command) Receipt {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
	id := cmd(Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "实现"}).CreatedID
	cmd(Command{OperationID: "start", Type: "start_task", ProjectID: p, TaskID: id})
	before := s.Snapshot()
	conv := before.Tasks[id].ConversationID
	execution := before.Tasks[id].ExecutionID
	s.Close()
	s, e = Open(db, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after := s.Snapshot()
	if after.Executions[execution].Phase != "interrupted" || after.Tasks[id].Status != "needs_confirmation" || after.Conversations[conv].Messages[0].Delivery != "not_sent" {
		t.Fatal("restart replayed or forgot execution")
	}
	if _, e = s.Command(Command{OperationID: "again", Type: "start_task", ProjectID: p, TaskID: id}); e == nil {
		t.Fatal("unknown delivery allowed replay")
	}
}
