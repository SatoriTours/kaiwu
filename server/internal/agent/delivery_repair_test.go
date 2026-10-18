package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func repairPolicyFixture(projectID string) workbench.ProjectDeliveryPolicy {
	p := workbench.DefaultDeliveryPolicy(projectID)
	p.Enabled = true
	p.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	p.Checks = []workbench.DeliveryCheck{{ID: "test", Argv: []string{"go", "test", "./..."}, Dir: ".", TimeoutSeconds: 300}}
	p.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	p.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2}}
	p.GitCredentialRef = "server_github_https"
	p.APICredentialRef = "server_github_api"
	p.AuthorizationDigest = "explicit-scope"
	p.Actor = "local-login"
	p.AuthorizedAt = time.Now().UTC()
	return p
}

type capturedPrompt struct {
	Message string `json:"message"`
	PID     int    `json:"pid"`
	Session string `json:"session"`
}

func capturePromptLines(t *testing.T, path string) []capturedPrompt {
	t.Helper()
	f, e := os.Open(path)
	if e != nil {
		if os.IsNotExist(e) {
			return nil
		}
		t.Fatal(e)
	}
	defer f.Close()
	var out []capturedPrompt
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var line capturedPrompt
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			t.Fatal("invalid prompt capture line")
		}
		out = append(out, line)
	}
	return out
}

func repairWaitRunState(t *testing.T, s *workbench.Store, conversation string, want string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		c := s.Snapshot().Conversations[conversation]
		switch c.RunState {
		case want:
			return
		case "failed", "interrupted":
			t.Fatalf("unexpected %q: %+v", c.RunState, c.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("conversation never reached %s: %+v", want, s.Snapshot().Conversations[conversation].RunState)
}

// deliveryRepairFixture builds a task conversation with a settled start round
// and an enabled delivery cycle, mirroring production 先有原会话再自动化交付.
func deliveryRepairFixture(t *testing.T, extraEnv string) (*Manager, *workbench.Store, string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	capture := filepath.Join(root, "prompts.log")
	source := filepath.Join(root, "project")
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, source, "init", "--template=")
	if e := os.WriteFile(filepath.Join(source, "README.md"), []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, source, "add", "README.md")
	workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")

	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{Provider: "fake", Model: "baseline"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "fixture", Path: source}).CreatedID
	task := command(workbench.Command{OperationID: "task", Type: "add_task", ProjectID: project, Title: "feature", Request: "要求"}).CreatedID
	if _, e = s.SaveDeliveryPolicy("enable-"+project, 0, repairPolicyFixture(project)); e != nil {
		t.Fatal(e)
	}
	start := command(workbench.Command{OperationID: "start", Type: "start_task", ProjectID: project, TaskID: task})
	if start.DeliveryID == "" {
		t.Fatal("no delivery cycle")
	}
	conversation := s.Snapshot().Tasks[task].ConversationID
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	m := New(context.Background(), s, Config{Program: exe, DataDir: filepath.Join(root, "data"), BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "KAIWU_AGENT_PROMPT_CAPTURE=" + capture}})
	if extraEnv != "" {
		m.cfg.ExtraEnv = append(m.cfg.ExtraEnv, extraEnv)
	}
	if e = m.Send(conversation, start.CreatedID); e != nil {
		t.Fatal(e)
	}
	repairWaitRunState(t, s, conversation, "idle")
	return m, s, project, task, start.DeliveryID, conversation
}

func TestDeliveryRepairSameSessionOnce(t *testing.T) {
	m, s, project, task, deliveryID, conversation := deliveryRepairFixture(t, "")
	capturePath := ""
	for _, env := range m.cfg.ExtraEnv {
		if strings.HasPrefix(env, "KAIWU_AGENT_PROMPT_CAPTURE=") {
			capturePath = strings.TrimPrefix(env, "KAIWU_AGENT_PROMPT_CAPTURE=")
		}
	}
	if capturePath == "" {
		t.Skip("capture disabled")
	}
	before := s.Snapshot().Conversations[conversation]
	if p, e := s.DeliveryPolicy(project); e != nil || !p.Enabled {
		t.Fatalf("delivery must be enabled: %+v %v", p, e)
	}
	if len(before.Messages) != 2 || before.SessionFile == "" {
		t.Fatalf("fixture start round incomplete: %+v", before)
	}
	if s.Snapshot().Tasks[task].ConversationID != conversation {
		t.Fatal("task conversation mismatch")
	}
	d, e := s.Delivery(deliveryID)
	if e != nil {
		t.Fatal(e)
	}
	next := d
	next.Phase = "blocked"
	next.CandidateSHA = "c0"
	d, e = s.AdvanceDelivery(d.ID, d.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}

	repair, e := s.PrepareDeliveryRepair(d.ID, d.Version, "c0", "task_ci", "fp-a", "任务 CI 断言失败")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.DispatchRepair(context.Background(), d.ID, repair.ID); e != nil {
		t.Fatal(e)
	}
	repairWaitRunState(t, s, conversation, "idle")

	after := s.Snapshot().Conversations[conversation]
	if after.SessionID != before.SessionID || after.SessionFile != before.SessionFile {
		t.Fatalf("repair must keep the same session identity: %+v -> %+v", before, after)
	}
	lines := capturePromptLines(t, capturePath)
	if len(lines) != 2 {
		t.Fatalf("expected exactly two dispatched requests, got %d", len(lines))
	}
	if lines[1].Session != before.SessionID || lines[1].Session != lines[0].Session {
		t.Fatalf("repair ran in a different session: %+v", lines)
	}
	if !strings.Contains(lines[1].Message, "任务 CI 断言失败") {
		t.Fatalf("repair prompt missing failure summary: %q", lines[1].Message)
	}
	var systemMessages int
	for _, msg := range after.Messages {
		if msg.Role == "system" {
			if msg.Kind != "automatic_repair" {
				t.Fatalf("system message kind: %+v", msg)
			}
			systemMessages++
		}
	}
	if systemMessages != 1 {
		t.Fatalf("expected one system repair message, got %d: %+v", systemMessages, after)
	}
	for _, r := range after.Rounds {
		if r.ID == repair.MessageID && r.Origin != "system_repair" {
			t.Fatalf("repair round origin: %+v", r)
		}
	}
	d2, e := s.Delivery(d.ID)
	if e != nil || d2.RepairsUsed != 1 {
		t.Fatalf("one dispatch must charge once: %+v %v", d2, e)
	}

	// Repeated dispatch of the same job while the request may be unknown:
	// never a second request, never another budget deduction.
	for range 2 {
		if e = m.DispatchRepair(context.Background(), d.ID, repair.ID); e != nil {
			t.Fatal(e)
		}
	}
	if lines2 := capturePromptLines(t, capturePath); len(lines2) != 2 {
		t.Fatalf("duplicate dispatch re-sent the request: %d prompts", len(lines2))
	}
	if d3, e := s.Delivery(d.ID); e != nil || d3.RepairsUsed != 1 {
		t.Fatalf("duplicate dispatch re-charged budget: %+v %v", d3, e)
	}
	final := s.Snapshot().Conversations[conversation]
	count := 0
	for _, msg := range final.Messages {
		if msg.Role == "system" && msg.Kind == "automatic_repair" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate dispatch appended system messages: %+v", final)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestDeliveryNextModelAtRoundBoundary(t *testing.T) {
	next := workbench.Profile{Provider: "fake", Model: "next", Thinking: "low", CredentialSource: "server_pi"}

	t.Run("applies at next round without touching the template", func(t *testing.T) {
		root := t.TempDir()
		s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "baseline"})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		command := func(c workbench.Command) workbench.Receipt {
			t.Helper()
			c.Revision = s.Snapshot().Revision
			r, e := s.Command(c)
			if e != nil {
				t.Fatal(e)
			}
			return r
		}
		p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: root}).CreatedID
		conv := s.Snapshot().Projects[p].ConversationID
		capture := filepath.Join(root, "prompts.log")
		exe, _ := os.Executable()
		m := New(context.Background(), s, Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "KAIWU_AGENT_PROMPT_CAPTURE=" + capture}})
		t.Cleanup(func() { m.Close() })

		r1 := command(workbench.Command{OperationID: "send-1", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "first"})
		if e = m.Send(conv, r1.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "running")
		// The user switches the model while round 1 is still running: the
		// current request must not be interrupted.
		if _, e := s.RequestNextProfile("next-op", conv, next); e != nil {
			t.Fatal(e)
		}
		if reply, e := s.RequestNextProfile("next-op", conv, next); e != nil || !reply.Replay {
			t.Fatalf("same operation replay: %+v %v", reply, e)
		}
		c := s.Snapshot().Conversations[conv]
		if c.Profile.Model != "baseline" || c.PendingProfile == nil || c.Rounds[0].Profile.Model != "baseline" {
			t.Fatalf("mid-request switch leaked: %+v", c)
		}
		repairWaitRunState(t, s, conv, "idle")
		if !strings.Contains(s.Snapshot().Conversations[conv].Messages[1].Text, "reply:first") {
			t.Fatal("current request was disturbed by the pending model switch")
		}

		r2 := command(workbench.Command{OperationID: "send-2", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "second"})
		if e = m.Send(conv, r2.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "idle")

		c = s.Snapshot().Conversations[conv]
		if c.Profile != next || c.PendingProfile != nil {
			t.Fatalf("pending model not applied after settle: %+v", c)
		}
		if template := s.Snapshot().Profile; template.Model != "baseline" {
			t.Fatalf("model switch leaked into the template: %+v", template)
		}
		lines := capturePromptLines(t, capture)
		if len(lines) != 2 || lines[1].PID == lines[0].PID {
			t.Fatalf("next round must run on a fresh instance with the new model: %+v", lines)
		}
		if c.Rounds[1].Profile != next {
			t.Fatalf("next round must record the actual profile: %+v", c.Rounds[1])
		}
		r3 := command(workbench.Command{OperationID: "send-3", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "third"})
		if e = m.Send(conv, r3.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "idle")
		lines = capturePromptLines(t, capture)
		if len(lines) != 3 || lines[2].PID != lines[1].PID {
			t.Fatalf("idle instances must be reused without a model relaunch: %+v", lines)
		}
		if e = m.Close(); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("failed verification never sends with the old model", func(t *testing.T) {
		root := t.TempDir()
		s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "baseline"})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		command := func(c workbench.Command) workbench.Receipt {
			t.Helper()
			c.Revision = s.Snapshot().Revision
			r, e := s.Command(c)
			if e != nil {
				t.Fatal(e)
			}
			return r
		}
		p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: root}).CreatedID
		conv := s.Snapshot().Projects[p].ConversationID
		capture := filepath.Join(root, "prompts.log")
		exe, _ := os.Executable()
		m := New(context.Background(), s, Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "KAIWU_AGENT_PROMPT_CAPTURE=" + capture, "KAIWU_AGENT_FAIL_MODEL=1"}})
		t.Cleanup(func() { m.Close() })

		r1 := command(workbench.Command{OperationID: "send-1", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "first"})
		if e = m.Send(conv, r1.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "idle")
		if _, e = s.RequestNextProfile("next-op", conv, next); e != nil {
			t.Fatal(e)
		}
		r2 := command(workbench.Command{OperationID: "send-2", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "second"})
		if e = m.Send(conv, r2.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "failed")

		c := s.Snapshot().Conversations[conv]
		if c.Profile.Model != "baseline" || c.PendingProfile == nil {
			t.Fatalf("pending model must survive a failed switch: %+v", c)
		}
		if lines := capturePromptLines(t, capture); len(lines) != 1 {
			t.Fatalf("failed switch must not silently send the prompt: %+v", lines)
		}
		if e = m.Close(); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("self slot keeps capacity available", func(t *testing.T) {
		root := t.TempDir()
		s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "baseline"})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		command := func(c workbench.Command) workbench.Receipt {
			t.Helper()
			c.Revision = s.Snapshot().Revision
			r, e := s.Command(c)
			if e != nil {
				t.Fatal(e)
			}
			return r
		}
		p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: root}).CreatedID
		conv := s.Snapshot().Projects[p].ConversationID
		exe, _ := os.Executable()
		m := New(context.Background(), s, Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}})
		t.Cleanup(func() { m.Close() })

		// Fill the 8-instance budget with the target plus seven neighbours.
		for i := range 7 {
			sp := command(workbench.Command{OperationID: "sp" + string(rune('a'+i)), Type: "add_project", Name: "sp" + string(rune('a'+i)), Path: filepath.Join(root, "sp"+string(rune('a'+i)))}).CreatedID
			other := s.Snapshot().Projects[sp].ConversationID
			r := command(workbench.Command{OperationID: "one" + string(rune('a'+i)), Type: "send_message", ProjectID: sp, ConversationID: other, Text: "mul" + string(rune('a'+i))})
			if e = m.Send(other, r.CreatedID); e != nil {
				t.Fatal(e)
			}
			repairWaitRunState(t, s, other, "idle")
		}
		r1 := command(workbench.Command{OperationID: "send-1", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "first"})
		if e = m.Send(conv, r1.CreatedID); e != nil {
			t.Fatal(e)
		}
		repairWaitRunState(t, s, conv, "idle")
		if m.Count() != 8 {
			t.Fatalf("expected a full house, got %d instances", m.Count())
		}
		if _, e = s.RequestNextProfile("next-op", conv, next); e != nil {
			t.Fatal(e)
		}
		r2 := command(workbench.Command{OperationID: "send-2", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "second"})
		if e = m.Send(conv, r2.CreatedID); e != nil {
			t.Fatalf("self-slot recycle must not be rejected by capacity: %v", e)
		}
		repairWaitRunState(t, s, conv, "idle")
		if c := s.Snapshot().Conversations[conv]; c.Profile != next || c.PendingProfile != nil {
			t.Fatalf("model not applied: %+v", c)
		}
		if e = m.Close(); e != nil {
			t.Fatal(e)
		}
	})
}
