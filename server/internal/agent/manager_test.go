package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFakePi(t *testing.T) {
	if os.Getenv("KAIWU_AGENT_HELPER") != "1" {
		t.Skip("helper")
	}
	session, file := fixtureSession()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var c map[string]any
		if json.Unmarshal(scanner.Bytes(), &c) != nil {
			os.Exit(2)
		}
		response := map[string]any{"type": "response", "id": c["id"], "command": c["type"], "success": true, "data": map[string]any{"isStreaming": false, "isCompacting": false, "sessionId": session, "sessionFile": file, "pendingMessageCount": 0}}
		if data, ok := response["data"].(map[string]any); ok {
			// Mirror the launch argv so a different profile launch is
			// verifiable without a real provider; absence means unknown.
			model := fixtureArg("--model")
			if os.Getenv("KAIWU_AGENT_FAIL_MODEL") == "1" {
				model = "wrong-model"
			}
			// RpcSessionState.model is a Model object, never a string.
			data["model"] = map[string]any{"id": model, "provider": fixtureArg("--provider"), "contextWindow": 200000}
		}
		if c["type"] == "prompt" {
			response["data"] = map[string]any{"disposition": "started"}
		}
		if c["type"] == "prompt" {
			if capture := os.Getenv("KAIWU_AGENT_PROMPT_CAPTURE"); capture != "" {
				f, err := os.OpenFile(capture, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					os.Exit(10)
				}
				_ = json.NewEncoder(f).Encode(map[string]any{"message": c["message"], "pid": os.Getpid(), "session": session})
				_ = f.Close()
			}
			c["message"] = strings.Split(c["message"].(string), "\n\n<kaiwu-reply-requirements>")[0]
			writeFixtureSession(session, file)
			if fixtureArg("--tools") == "read,write,edit,bash,grep,find,ls" {
				if os.Getenv("KAIWU_AGENT_LEAK") == "1" {
					exe, _ := os.Executable()
					child := exec.Command(exe, "-test.run=^TestPersistentWriterHelper$")
					child.Env = []string{"KAIWU_WRITER_HELPER=1", "GORACE=atexit_sleep_ms=0"}
					child.Stdout = os.Stdout
					child.Stderr = os.Stderr
					if child.Start() != nil {
						os.Exit(9)
					}
					_ = os.WriteFile("writer.pid", []byte(fmt.Sprint(child.Process.Pid)), 0600)
				}
				_ = os.WriteFile(filepath.Join(mustCwd(), "kaiwu-fixture-result.txt"), []byte(c["message"].(string)), 0600)
				fmt.Println(`{"type":"tool_execution_start","toolCallId":"fixture-write","toolName":"write","args":{"path":"kaiwu-fixture-result.txt"}}`)
				fmt.Println(`{"type":"tool_execution_end","toolCallId":"fixture-write","toolName":"write","result":{"content":[{"type":"text","text":"fixture written"}]},"isError":false}`)
			}
		}
		b, _ := json.Marshal(response)
		late := c["type"] == "prompt" && strings.HasPrefix(c["message"].(string), "late:")
		if !late {
			fmt.Println(string(b))
		}
		originalResponse := string(b)
		if c["type"] == "prompt" {
			if c["message"] == "review-stale" {
				for _, delta := range []string{`safe summary<kaiwu-report>{"summary":"success","stage":"awaiting_review"}</kaiwu-report>`, "IMPORTANT FAILURE<kaiwu-report>{"} {
					b, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]string{"type": "text_delta", "delta": delta}})
					fmt.Println(string(b))
				}
			}

			if c["message"] == "overflow" {
				for i := 0; i < 2; i++ {
					b, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]string{"type": "text_delta", "delta": strings.Repeat("x", 600000)}})
					fmt.Println(string(b))
				}
			}
			if strings.HasPrefix(c["message"].(string), "retry:") {
				fmt.Println(`{"type":"message_end","message":{"role":"assistant","stopReason":"error"}}`)
				time.Sleep(80 * time.Millisecond)
			}
			time.Sleep(40 * time.Millisecond)
			b, _ = json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "reply:" + c["message"].(string)}})
			fmt.Println(string(b))
			fmt.Println(`{"type":"message_end","message":{"role":"assistant","stopReason":"stop"}}`)
			fmt.Println(`{"type":"agent_settled"}`)
			if late {
				time.Sleep(100 * time.Millisecond)
				_ = os.WriteFile(filepath.Join(mustCwd(), "ack.marker"), []byte("ready"), 0600)
				fmt.Println(originalResponse)
			}
		}
	}
	os.Exit(0)
}
func TestIndependentLazySessionsAndDurableReplies(t *testing.T) {
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "s.db"), workbench.Profile{Provider: "fake", Model: "fake"})
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
	a := command(workbench.Command{OperationID: "task-a", Type: "add_task", ProjectID: p, Request: "A"}).CreatedID
	b := command(workbench.Command{OperationID: "task-b", Type: "add_task", ProjectID: p, Request: "B"}).CreatedID
	executable, _ := os.Executable()
	m := New(context.Background(), s, Config{Program: executable, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}})
	defer m.Close()
	if m.Count() != 0 {
		t.Fatal("select/create must not start CLI")
	}
	for _, id := range []string{a, b} {
		conv := s.Snapshot().Tasks[id].ConversationID
		text := "retry:" + id
		if id == b {
			text = "late:" + id
		}
		r := command(workbench.Command{OperationID: "send-" + id, Type: "send_message", ProjectID: p, ConversationID: conv, Text: text})
		if e = m.Send(conv, r.CreatedID); e != nil {
			t.Fatal(e)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot := s.Snapshot()
		done := true
		for _, id := range []string{a, b} {
			c := snapshot.Conversations[snapshot.Tasks[id].ConversationID]
			if c.RunState == "failed" {
				t.Fatal("transient provider error must remain running until settled")
			}
			if c.RunState != "idle" {
				done = false
				continue
			}
			if id == b {
				if _, e := os.Stat(filepath.Join(root, "sessions", c.ID, "ack.marker")); e != nil {
					t.Fatal("settled cannot release unacknowledged prompt")
				}
			}
			if len(c.Messages) != 2 || !strings.Contains(c.Messages[1].Text, id) {
				t.Fatalf("wrong history: %+v", c)
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sessions did not settle", snapshot.Conversations)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Count() != 2 {
		t.Fatal("sessions should remain independently alive")
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if m.Count() != 0 {
		t.Fatal("close did not reap")
	}
}

func fixtureSession() (string, string) {
	session, file := "", ""
	for i, arg := range os.Args {
		if i+1 < len(os.Args) {
			if arg == "--session-id" {
				session = os.Args[i+1]
			}
			if arg == "--session" {
				file = os.Args[i+1]
			}
		}
	}
	if file == "" {
		dir := fixtureArg("--session-dir")
		if dir == "" {
			dir = mustCwd()
		}
		file = filepath.Join(dir, session+".jsonl")

	} else {
		b, _ := os.ReadFile(file)
		var v struct{ ID string }
		json.Unmarshal([]byte(strings.Split(string(b), "\n")[0]), &v)
		session = v.ID
	}
	return session, file
}
func mustCwd() string { p, _ := os.Getwd(); return p }
func fixtureArg(name string) string {
	for i, arg := range os.Args {
		if arg == name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func TestRecoverAfterReplyLimitAndRejectMissingSession(t *testing.T) {
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cmd := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: root}).CreatedID
	conv := s.Snapshot().Projects[p].ConversationID
	exe, _ := os.Executable()
	cfg := Config{Program: exe, DataDir: root, BaseArgs: []string{"-test.run=TestFakePi", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}}
	m := New(context.Background(), s, cfg)
	defer m.Close()
	wait := func(state string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if s.Snapshot().Conversations[conv].RunState == state {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("expected %s, got %s", state, s.Snapshot().Conversations[conv].RunState)
	}
	r := cmd(workbench.Command{OperationID: "overflow-send", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "overflow"})
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait("interrupted")
	cmd(workbench.Command{OperationID: "resume-op", Type: "resume", ProjectID: p, ConversationID: conv})
	if e = m.Resume(conv); e != nil {
		t.Fatal(e)
	}
	wait("idle")
	r = cmd(workbench.Command{OperationID: "next-send", Type: "send_message", ProjectID: p, ConversationID: conv, Text: "next"})
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	wait("idle")
	if !strings.Contains(s.Snapshot().Conversations[conv].Messages[len(s.Snapshot().Conversations[conv].Messages)-1].Text, "reply:next") {
		t.Fatal("recovered consumer missing")
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	c := s.Snapshot().Conversations[conv]
	file := c.SessionFile
	if file == "" {
		t.Fatal("session identity not persisted")
	}
	if e = os.Remove(file); e != nil {
		t.Fatal(e)
	}
	m2 := New(context.Background(), s, cfg)
	defer m2.Close()
	cmd(workbench.Command{OperationID: "missing-resume", Type: "resume", ProjectID: p, ConversationID: conv})
	if e = m2.Resume(conv); e != nil {
		t.Fatal(e)
	}
	wait("failed")
	if _, e = os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("missing context silently recreated")
	}
}

func writeFixtureSession(session, file string) {
	b, _ := json.Marshal(map[string]any{"type": "session", "version": 3, "id": session})
	_ = os.WriteFile(file, append(b, '\n'), 0600)
}
