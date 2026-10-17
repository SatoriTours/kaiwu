package workbench

import (
	"path/filepath"
	"testing"
)

func TestNewUserIntentAutomaticallyReopensInterruptedTaskWithoutReplayingOldIntent(t *testing.T) {
	for _, kind := range []string{"send_message", "start_task", "legacy_start"} {
		t.Run(kind, func(t *testing.T) {
			s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{Provider: "fake", Model: "fake"})
			if e != nil {
				t.Fatal(e)
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
			p := cmd(Command{OperationID: "auto-project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
			id := cmd(Command{OperationID: "auto-task", Type: "add_task", ProjectID: p, Request: "fixture"}).CreatedID
			cmd(Command{OperationID: "first", Type: "start_task", ProjectID: p, TaskID: id})
			before := s.Snapshot()
			task := before.Tasks[id]
			conv := task.ConversationID
			xID := task.ExecutionID
			s.Update(func(st *State) error {
				c := st.Conversations[conv]
				c.RunState = "interrupted"
				c.Messages[0].Delivery = "uncertain"
				st.Conversations[conv] = c
				task := st.Tasks[id]
				task.Status = "interrupted"
				if kind == "legacy_start" {
					task.Status = "needs_confirmation"
				}
				st.Tasks[id] = task
				x := st.Executions[xID]
				x.Phase = "interrupted"
				if kind == "legacy_start" {
					x.Phase = "awaiting_review"
				}
				x.Workspace = "existing-worktree"
				x.Tools = []ToolRecord{{ID: "old", Name: "write", Result: "saved"}}
				st.Executions[xID] = x
				return nil
			})
			text := "继续开发，只执行新的要求"
			if kind == "start_task" || kind == "legacy_start" {
				text = "开始开发"
			}
			commandType := kind
			if kind == "legacy_start" {
				commandType = "start_task"
			}
			c := Command{OperationID: "continue", Type: commandType, ProjectID: p, TaskID: id, ConversationID: conv, Text: text, Revision: s.Snapshot().Revision}
			r, e := s.Command(c)
			if e != nil {
				t.Fatal("new user intent still requires manual recovery", e)
			}
			if _, e = s.Command(c); e != nil {
				t.Fatal(e)
			}
			after := s.Snapshot()
			if after.Tasks[id].ExecutionID != xID || after.Executions[xID].Workspace != "existing-worktree" || len(after.Executions[xID].Tools) != 1 {
				t.Fatal("lost existing task execution")
			}
			messages := after.Conversations[conv].Messages
			if len(messages) != 2 || messages[0].Delivery != "uncertain" || messages[1].ID != r.CreatedID || messages[1].Text != text {
				t.Fatal("replayed or changed old intent", messages)
			}
		})
	}
}
