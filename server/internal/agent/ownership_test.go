//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestPersistentWriterHelper(t *testing.T) {
	if os.Getenv("KAIWU_WRITER_HELPER") != "1" {
		t.Skip("helper")
	}
	for {
		_ = os.WriteFile("old-writer.txt", []byte(time.Now().String()), 0600)
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCrashedExecutionHostHelper(t *testing.T) {
	if os.Getenv("KAIWU_CRASH_HOST") != "1" {
		t.Skip("helper")
	}
	root := os.Getenv("KAIWU_CRASH_ROOT")
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		os.Exit(2)
	}
	cmd := func(c workbench.Command) workbench.Receipt {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			os.Exit(3)
		}
		return r
	}
	p := cmd(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: filepath.Join(root, "project")}).CreatedID
	a := cmd(workbench.Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "write fixture"}).CreatedID
	exe, _ := os.Executable()
	m := New(context.Background(), s, Config{Program: exe, DataDir: filepath.Join(root, "data"), BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "KAIWU_AGENT_LEAK=1", "GORACE=atexit_sleep_ms=0"}})
	r := cmd(workbench.Command{OperationID: "start", Type: "start_task", ProjectID: p, TaskID: a})
	if m.Send(r.ConversationID, r.CreatedID) != nil {
		os.Exit(4)
	}
	for {
		st := s.Snapshot()
		x := st.Executions[st.Tasks[a].ExecutionID]
		if x.Phase == "awaiting_review" {
			b, _ := json.Marshal(map[string]string{"task": a, "project": p, "conversation": r.ConversationID, "workspace": x.Workspace})
			_ = os.WriteFile(filepath.Join(root, "ready.json"), b, 0600)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {}
}

func TestRecoveryRefusesOrphanedWriterAfterHostKill(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	_ = os.Mkdir(source, 0700)
	workspaceGit(t, source, "init", "--template=")
	exe, _ := os.Executable()
	host := exec.Command(exe, "-test.run=^TestCrashedExecutionHostHelper$")
	host.Env = append(os.Environ(), "KAIWU_CRASH_HOST=1", "KAIWU_CRASH_ROOT="+root, "GORACE=atexit_sleep_ms=0")
	if e := host.Start(); e != nil {
		t.Fatal(e)
	}
	defer host.Process.Kill()
	var ready map[string]string
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		b, _ := os.ReadFile(filepath.Join(root, "ready.json"))
		if json.Unmarshal(b, &ready) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ready == nil {
		_ = host.Process.Kill()
		_ = host.Wait()
		t.Fatal("host fixture did not start")
	}
	b, e := os.ReadFile(filepath.Join(ready["workspace"], "writer.pid"))
	if e != nil {
		t.Fatal(e)
	}
	writerPID, _ := strconv.Atoi(string(b))
	if writerPID <= 0 {
		t.Fatal("writer missing")
	}
	t.Cleanup(func() { p, _ := os.FindProcess(writerPID); _ = p.Kill() })
	_ = host.Process.Kill()
	_ = host.Wait()
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m := New(context.Background(), s, Config{Program: exe, DataDir: filepath.Join(root, "data"), BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "GORACE=atexit_sleep_ms=0"}})
	defer m.Close()
	_, e = s.Command(workbench.Command{OperationID: "resume", Type: "resume", ProjectID: ready["project"], ConversationID: ready["conversation"]})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Resume(ready["conversation"]); e != nil {
		t.Fatal(e)
	}
	blocked := false
	for time.Now().Before(end) {
		c := s.Snapshot().Conversations[ready["conversation"]]
		if c.RunState == "idle" {
			t.Fatal("restored a second writer while old child still lives")
		}
		if c.RunState == "failed" || c.RunState == "interrupted" {
			if m.Count() != 0 {
				t.Fatal("started process despite orphan")
			}
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("recovery did not refuse orphan")
	}
	writer, _ := os.FindProcess(writerPID)
	_ = writer.Kill()
	for time.Now().Before(end) {
		b, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(writerPID), "stat"))
		if len(b) == 0 || strings.Contains(string(b), ") Z ") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, e = s.Command(workbench.Command{OperationID: "resume-clean", Type: "resume", ProjectID: ready["project"], ConversationID: ready["conversation"]})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Resume(ready["conversation"]); e != nil {
		t.Fatal(e)
	}
	for time.Now().Before(end) {
		c := s.Snapshot().Conversations[ready["conversation"]]
		if c.RunState == "failed" || c.RunState == "interrupted" {
			t.Fatal("clean recovery failed", c.Error)
		}
		if c.RunState == "idle" {
			if len(c.Messages) != 2 {
				t.Fatal("replayed old start")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("clean recovery did not complete")
}

func TestNeverDispatchedStartRecoversWithoutReplaying(t *testing.T) {
	m, _, _, _ := workspaceFixture(t)
	s := m.store
	_ = s.Update(func(st *workbench.State) error {
		st.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		return nil
	})
	r, e := s.Command(workbench.Command{OperationID: "create-task", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: "p", Request: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	a := r.CreatedID
	conv := s.Snapshot().Tasks[a].ConversationID
	sid := s.Snapshot().Conversations[conv].SessionID
	_, e = s.Command(workbench.Command{OperationID: "start", Type: "start_task", ProjectID: "p", TaskID: a})
	if e != nil {
		t.Fatal(e)
	}
	_ = m.Close()
	_ = s.Close()
	s, e = workbench.Open(filepath.Join(m.cfg.DataDir, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	exe, _ := os.Executable()
	m2 := New(context.Background(), s, Config{Program: exe, DataDir: m.cfg.DataDir, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}})
	defer m2.Close()
	_, e = s.Command(workbench.Command{OperationID: "resume", Type: "resume", ProjectID: "p", ConversationID: conv})
	if e != nil {
		t.Fatal(e)
	}
	if e = m2.Resume(conv); e != nil {
		t.Fatal(e)
	}
	end := time.Now().Add(6 * time.Second)
	for time.Now().Before(end) {
		c := s.Snapshot().Conversations[conv]
		if c.RunState == "failed" || c.RunState == "interrupted" {
			t.Fatal(c.Error)
		}
		if c.RunState == "idle" {
			if c.SessionID != sid || len(c.Messages) != 1 || c.Messages[0].Delivery != "not_sent" || s.Snapshot().Tasks[a].Status != "idle" {
				t.Fatal("replayed or lost unsent identity", c)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Snapshot().Conversations[conv].RunState != "idle" {
		t.Fatal("resume did not finish")
	}
	r, e = s.Command(workbench.Command{OperationID: "start-explicitly", Type: "start_task", ProjectID: "p", TaskID: a})
	if e != nil {
		t.Fatal(e)
	}
	if e = m2.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	for time.Now().Before(end) {
		c := s.Snapshot().Conversations[conv]
		if c.RunState == "failed" || c.RunState == "interrupted" {
			t.Fatal(c.Error)
		}
		if c.RunState == "idle" {
			if len(c.Messages) != 3 {
				t.Fatal("wrong explicit start history", c.Messages)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("explicit new start did not settle")
}

func TestUnconfirmedLaunchLeaseFailsClosed(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "tree")
	if e := os.Mkdir(workspace, 0700); e != nil {
		t.Fatal(e)
	}
	l, e := acquireExecutionLease(workspace)
	if e != nil {
		t.Fatal(e)
	}
	l.attempt()
	if e = l.release(false); e != nil {
		t.Fatal(e)
	}
	if again, e := acquireExecutionLease(workspace); e == nil {
		_ = again.release(false)
		t.Fatal("unknown launch ownership silently discarded")
	}
}
