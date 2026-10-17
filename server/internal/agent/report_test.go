package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInvalidFinalReportFallsBackToOriginal(t *testing.T) {
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "s.db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Command(workbench.Command{OperationID: "project-review", Type: "add_project", Name: "p", Path: root, Revision: s.Snapshot().Revision})
	if e != nil {
		t.Fatal(e)
	}
	conv := s.Snapshot().Projects[p.CreatedID].ConversationID
	r, e := s.Command(workbench.Command{OperationID: "send-review", Type: "send_message", ProjectID: p.CreatedID, ConversationID: conv, Text: "review-stale", Revision: s.Snapshot().Revision})
	if e != nil {
		t.Fatal(e)
	}
	exe, _ := os.Executable()
	m := New(context.Background(), s, Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}})
	defer m.Close()
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c := s.Snapshot().Conversations[conv]
		if c.RunState == "idle" {
			msg := c.Messages[1]
			if msg.DisplayText != nil || c.Rounds[0].Report != nil || c.Rounds[0].ReportError == "" {
				t.Fatalf("invalid final report retains displayText=%v; raw=%q; report=%+v", msg.DisplayText, msg.Text, c.Rounds[0].Report)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func TestFailedRoundHasTerminalPhase(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Update(func(st *workbench.State) error {
		st.Conversations["c"] = workbench.Conversation{ID: "c", RunState: "running"}
		workbench.BeginRound(st, "c", "r")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	m := New(context.Background(), s, Config{})
	defer m.Close()
	m.failed("c", "fixture failure", true)
	r := s.Snapshot().Conversations["c"].Rounds[0]
	if r.Phase != "interrupted" || r.FinishedAt == "" {
		t.Fatal("round not terminated", r)
	}
}

func TestInterruptedExecutionDoesNotAskForResultAcceptance(t *testing.T) {
	st := workbench.State{Tasks: map[string]workbench.Task{"t": {ID: "t", ConversationID: "c", ExecutionID: "x", Status: "running"}}, Conversations: map[string]workbench.Conversation{"c": {ID: "c", TaskID: "t", RunState: "running"}}, Executions: map[string]workbench.Execution{"x": {ID: "x", TaskID: "t", ConversationID: "c", Phase: "running"}}}
	setExecutionPhase(&st, "c", "interrupted", "fixture stop")
	if st.Tasks["t"].Status != "interrupted" {
		t.Fatal("interrupted work presented as result ready", st.Tasks["t"])
	}
}
func TestFinishedResultSurvivesIdleSessionLoss(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Update(func(st *workbench.State) error {
		st.Tasks["t"] = workbench.Task{ID: "t", ConversationID: "c", ExecutionID: "x", Status: "needs_confirmation"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", TaskID: "t", RunState: "idle"}
		st.Executions["x"] = workbench.Execution{ID: "x", TaskID: "t", ConversationID: "c", Phase: "awaiting_review", Summary: "done", FinishedAt: "2026-10-14T00:00:00Z"}
		return nil
	})
	m := New(context.Background(), s, Config{})
	defer m.Close()
	m.failed("c", "idle session disconnected", true)
	st := s.Snapshot()
	if st.Executions["x"].Phase != "awaiting_review" || st.Tasks["t"].Status != "needs_confirmation" {
		t.Fatal("session loss destroyed finished result", st.Executions["x"], st.Tasks["t"])
	}
}
