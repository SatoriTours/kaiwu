//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestTaskPreviewHTTPHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "task-preview" {
		return
	}
	ln, e := net.Listen("tcp", "127.0.0.1:"+os.Args[len(os.Args)-1])
	if e != nil {
		os.Exit(2)
	}
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "real task preview") }))
	os.Exit(0)
}

func TestTaskPreviewRetainsSessionAndStopsBeforeNewDevelopment(t *testing.T) {
	m, _, _, _ := workspaceFixture(t)
	s := m.store
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	id := command(workbench.Command{OperationID: "create-preview", Type: "add_task", ProjectID: "p", Title: "preview", Request: "fixture"}).CreatedID
	conv := s.Snapshot().Tasks[id].ConversationID
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
	r := command(workbench.Command{OperationID: "dev-preview", Type: "start_task", ProjectID: "p", TaskID: id})
	if e := m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait()
	before := s.Snapshot()
	x := before.Executions[before.Tasks[id].ExecutionID]
	old := before.Conversations[conv]
	manifest, _ := json.Marshal(map[string]any{"version": 1, "command": []string{exe, "-test.run=TestTaskPreviewHTTPHelper", "--", "task-preview", "{port}"}, "path": "/"})
	_ = os.WriteFile(filepath.Join(x.Workspace, "kaiwu-preview.json"), manifest, 0600)
	pm := preview.New(context.Background(), s, preview.Config{DataDir: m.cfg.DataDir, Reserve: m.AcquirePreviewLease})
	defer pm.Close()
	m.SetPreviewStopper(pm.StopTask)
	req := preview.Request{ProjectID: "p", TaskID: id, RoundID: old.Rounds[len(old.Rounds)-1].ID, OperationID: "start-preview"}
	shown, err := pm.Status(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ManifestID = shown.ManifestID
	if v, e := pm.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	after := s.Snapshot()
	if after.Conversations[conv].SessionID != old.SessionID || after.Conversations[conv].SessionFile != old.SessionFile || after.Tasks[id].Status != "needs_confirmation" || m.Count() != 0 {
		t.Fatal("preview changed original context/result or did not release pi")
	}
	r = command(workbench.Command{OperationID: "modify-preview", Type: "send_message", ProjectID: "p", TaskID: id, ConversationID: conv, Text: "新的修改"})
	if e := m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait()
	after = s.Snapshot()
	if after.Conversations[conv].SessionID != old.SessionID || after.Conversations[conv].SessionFile != old.SessionFile || len(after.Conversations[conv].Messages) != len(old.Messages)+2 {
		t.Fatal("new development replaced/replayed session")
	}
	req.RoundID = after.Conversations[conv].Rounds[len(after.Conversations[conv].Rounds)-1].ID
	if v, e := pm.Status(req); e != nil || v.State == "running" {
		t.Fatal("preview survived new development", v, e)
	}
}

func TestTaskMergeStopsPreviewBeforeTakingWriterLease(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	_ = m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.Rounds = []workbench.Round{{ID: "finished", Phase: "finished", FinishedAt: "done"}}
		st.Conversations[c.ID] = c
		v := st.Executions[x.ID]
		v.Phase = "awaiting_review"
		v.FinishedAt = "done"
		st.Executions[x.ID] = v
		return nil
	})
	exe, _ := os.Executable()
	b, _ := json.Marshal(map[string]any{"version": 1, "command": []string{exe, "-test.run=TestTaskPreviewHTTPHelper", "--", "task-preview", "{port}"}, "path": "/"})
	_ = os.WriteFile(filepath.Join(x.Workspace, "kaiwu-preview.json"), b, 0600)
	pm := preview.New(context.Background(), m.store, preview.Config{DataDir: m.cfg.DataDir, Reserve: m.AcquirePreviewLease})
	defer pm.Close()
	m.SetPreviewStopper(pm.StopTask)
	req := preview.Request{ProjectID: task.ProjectID, TaskID: task.ID, RoundID: "finished", OperationID: "preview-for-merge"}
	shown, err := pm.Status(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ManifestID = shown.ManifestID
	if v, e := pm.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	proof, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !proof.CanMerge {
		t.Fatal(proof, e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "merge-with-preview", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: proof.ID, CommitMessage: "fixture preview"})
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatal("preview prevented safe merge", op)
	}
	if _, e = os.Stat(filepath.Join(source, "kaiwu-preview.json")); e != nil {
		t.Fatal("changes not merged", e)
	}
	if v, e := pm.Status(req); e != nil || v.State != "stopped" {
		t.Fatal("preview survived integration", v, e)
	}
}
