package agent

import (
	"context"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplyRequirementsReachEveryPromptIncludingWarmAndRestoredSession(t *testing.T) {
	root := t.TempDir()
	store, err := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = store.Snapshot().Revision
		r, e := store.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := command(workbench.Command{Type: "add_project", OperationID: "project", Name: "测试", Path: root}).CreatedID
	conv := store.Snapshot().Projects[project].ConversationID
	executable, _ := os.Executable()
	capture := filepath.Join(root, "requests.jsonl")
	cfg := Config{Program: executable, DataDir: root, BaseArgs: []string{"-test.run=^TestFakePi$", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1", "KAIWU_AGENT_PROMPT_CAPTURE=" + capture}}
	manager := New(context.Background(), store, cfg)
	defer func() { _ = manager.Close() }()
	for i, text := range []string{"检查接口", "继续验证", "接回原会话"} {
		if i == 2 {
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			manager = New(context.Background(), store, cfg)
		}
		receipt := command(workbench.Command{Type: "send_message", OperationID: text, ProjectID: project, ConversationID: conv, Text: text})
		if err := manager.Send(conv, receipt.CreatedID); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for store.Snapshot().Conversations[conv].RunState != "idle" {
			if time.Now().After(deadline) {
				t.Fatal("prompt did not settle")
			}
			time.Sleep(10 * time.Millisecond)
		}
		messages := store.Snapshot().Conversations[conv].Messages
		if messages[len(messages)-2].Text != text {
			t.Fatal("stored user intent was changed")
		}
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected exactly three explicit user prompts, got %d", len(lines))
	}
	var requests []struct {
		Message string
		PID     int
		Session string
	}
	for _, line := range lines {
		var request struct {
			Message string
			PID     int
			Session string
		}
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		for _, requirement := range []string{"<kaiwu-reply-requirements>", "简体中文", "过程更新", "Markdown", "URL"} {
			if !strings.Contains(request.Message, requirement) {
				t.Fatalf("prompt is missing %q", requirement)
			}
		}
	}
	if requests[0].PID != requests[1].PID {
		t.Fatal("warm session unexpectedly replaced")
	}
	if requests[0].Session != requests[2].Session {
		t.Fatal("restore replaced original context")
	}
}
