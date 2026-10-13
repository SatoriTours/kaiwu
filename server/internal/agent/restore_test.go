package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreshManagerRestoresWithoutReplay(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "db")
	profile := workbench.Profile{}
	s, e := workbench.Open(db, profile)
	if e != nil {
		t.Fatal(e)
	}
	cmd := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(workbench.Command{OperationID: "review-project", Type: "add_project", Name: "p", Path: root}).CreatedID
	conv := s.Snapshot().Projects[p].ConversationID
	exe, _ := os.Executable()
	cfg := Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}}
	m := New(context.Background(), s, cfg)
	t.Cleanup(func() { m.Close(); s.Close() })
	wait := func(state string) {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			if s.Snapshot().Conversations[conv].RunState == state {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("expected %s got %+v", state, s.Snapshot().Conversations[conv])
	}
	r := cmd(workbench.Command{OperationID: "review-send-before", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "before restart"})
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait("idle")
	before := s.Snapshot().Conversations[conv]
	if len(before.Messages) != 2 || before.SessionFile == "" {
		t.Fatalf("initial conversation %+v", before)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = workbench.Open(db, profile)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.Snapshot().Conversations[conv].RunState != "interrupted" {
		t.Fatal("restart did not require explicit recovery")
	}
	m = New(context.Background(), s, cfg)
	defer m.Close()
	if m.Count() != 0 {
		t.Fatal("reopen started process")
	}
	cmd(workbench.Command{OperationID: "review-resume", Type: "resume", ProjectID: p, ConversationID: conv})
	if e = m.Resume(conv); e != nil {
		t.Fatal(e)
	}
	wait("idle")
	after := s.Snapshot().Conversations[conv]
	if after.SessionID != before.SessionID || after.SessionFile != before.SessionFile || len(after.Messages) != 2 {
		t.Fatalf("restore changed identity or replayed: %+v", after)
	}
	r = cmd(workbench.Command{OperationID: "review-send-after", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "after restart"})
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait("idle")
	if len(s.Snapshot().Conversations[conv].Messages) != 4 {
		t.Fatalf("new consumer failed: %+v", s.Snapshot().Conversations[conv])
	}
}
