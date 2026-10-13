package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestStartMovesOriginalSessionToWorktreeAndRestoresWithoutReplay(t *testing.T) {
	m, _, _, source := workspaceFixture(t)
	s := m.store
	cmd := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	a := cmd(workbench.Command{OperationID: "task-create", Type: "add_task", ProjectID: "p", Title: "开发测试", Request: "实现固定文件"}).CreatedID
	conv := s.Snapshot().Tasks[a].ConversationID
	_ = s.Update(func(st *workbench.State) error {
		c := st.Conversations[conv]
		c.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		st.Conversations[conv] = c
		return nil
	})
	exe, _ := os.Executable()
	m.cfg.Program = exe
	m.cfg.BaseArgs = []string{"-test.run=TestFakePi", "--"}
	m.cfg.ExtraEnv = []string{"KAIWU_AGENT_HELPER=1"}
	wait := func() {
		t.Helper()
		end := time.Now().Add(6 * time.Second)
		for time.Now().Before(end) {
			c := s.Snapshot().Conversations[conv]
			if c.RunState == "idle" {
				return
			}
			if c.RunState == "failed" || c.RunState == "interrupted" {
				t.Fatal(c.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("did not settle")
	}
	r := cmd(workbench.Command{OperationID: "discuss", Type: "send_message", ProjectID: "p", ConversationID: conv, Text: "之前的讨论"})
	if e := m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait()
	before := s.Snapshot().Conversations[conv]
	r = cmd(workbench.Command{OperationID: "start", Type: "start_task", ProjectID: "p", TaskID: a, ConversationID: conv})
	if e := m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait()
	st := s.Snapshot()
	after := st.Conversations[conv]
	task := st.Tasks[a]
	x := st.Executions[task.ExecutionID]
	if before.SessionID != after.SessionID || before.SessionFile != after.SessionFile || len(after.Messages) != 4 {
		t.Fatal("original context replaced", after)
	}
	if task.Status != "needs_confirmation" || x.Phase != "awaiting_review" || x.Summary == "" || len(x.Tools) == 0 {
		t.Fatal("missing truthful result", task, x)
	}
	if b, e := os.ReadFile(filepath.Join(x.Workspace, "kaiwu-fixture-result.txt")); e != nil || !strings.Contains(string(b), "实现固定文件") {
		t.Fatal("development did not write task file", e)
	}
	if _, e := os.Stat(filepath.Join(source, "kaiwu-fixture-result.txt")); !os.IsNotExist(e) {
		t.Fatal("wrote source directory")
	}
	if m.Count() != 1 {
		t.Fatal("created second task session")
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	var reopenErr error
	s, reopenErr = workbench.Open(filepath.Join(m.cfg.DataDir, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if reopenErr != nil {
		t.Fatal(reopenErr)
	}
	defer s.Close()
	if s.Snapshot().Conversations[conv].RunState != "interrupted" {
		t.Fatal("restart did not require explicit recovery")
	}
	m2 := New(context.Background(), s, m.cfg)
	defer m2.Close()
	cmd(workbench.Command{OperationID: "resume-dev", Type: "resume", ProjectID: "p", ConversationID: conv})
	if e := m2.Resume(conv); e != nil {
		t.Fatal(e)
	}
	wait()
	if len(s.Snapshot().Conversations[conv].Messages) != 4 {
		t.Fatal("resume replayed prompt")
	}
	if s.Snapshot().Executions[task.ExecutionID].Workspace != x.Workspace {
		t.Fatal("changed worktree")
	}
}
