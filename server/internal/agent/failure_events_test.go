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

// rateLimitReason is the provider envelope shape captured from a real failed
// round (identifiers replaced): pi forwards it verbatim as errorMessage.
const rateLimitReason = `429 {"type":"error","error":{"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)"},"request_id":"synthetic-request-id"}`

func failureFixture(t *testing.T) (*Manager, *workbench.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	store, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = store.Snapshot().Revision
		r, e := store.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "失败原因", Path: root}).CreatedID
	conversation := store.Snapshot().Projects[project].ConversationID
	executable, _ := os.Executable()
	m := New(context.Background(), store, Config{Program: executable, DataDir: root, BaseArgs: []string{"-test.run=^TestFakePi$", "--"}, ExtraEnv: []string{"KAIWU_AGENT_HELPER=1"}})
	t.Cleanup(func() { _ = m.Close() })
	return m, store, project, conversation
}

func sendRound(t *testing.T, m *Manager, store *workbench.Store, project, conversation, operation, text string) workbench.Conversation {
	t.Helper()
	c := workbench.Command{OperationID: operation, Type: "send_message", ProjectID: project, ConversationID: conversation, Text: text}
	c.Revision = store.Snapshot().Revision
	r, e := store.Command(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Send(conversation, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state := store.Snapshot().Conversations[conversation].RunState
		if state != "running" && state != "starting" {
			return store.Snapshot().Conversations[conversation]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("round %q did not settle: %+v", operation, store.Snapshot().Conversations[conversation])
	return workbench.Conversation{}
}

func TestFailedRoundShowsProviderReasonInsteadOfGenericText(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "fail-round", "fail:"+rateLimitReason)
	if got.RunState != "failed" {
		t.Fatalf("failure state changed: %q", got.RunState)
	}
	if !strings.Contains(got.Error, "已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)") {
		t.Fatalf("provider reason missing from failure text: %q", got.Error)
	}
	if !strings.Contains(got.Error, "429") {
		t.Fatalf("http status missing from failure text: %q", got.Error)
	}
	t.Logf("stored failure text: %s", got.Error)
	for _, leak := range []string{"synthetic-request-id", "rate_limit_error", "{\"type\""} {
		if strings.Contains(got.Error, leak) {
			t.Fatalf("raw provider envelope stored (%q): %q", leak, got.Error)
		}
	}
}

func TestFailedRoundWithoutPiReasonKeepsAccurateFallback(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "fail-silent", "fail:")
	if got.RunState != "failed" {
		t.Fatalf("failure state changed: %q", got.RunState)
	}
	if got.Error != "模型调用失败或被中断；已有内容保留" {
		t.Fatalf("fallback changed: %q", got.Error)
	}
}

func TestAbortedRoundDoesNotReportFixedAbortNoticeAsReason(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "abort-round", "abort:stop now")
	if got.RunState != "failed" {
		t.Fatalf("aborted round classification changed: %q", got.RunState)
	}
	if got.Error != "模型调用失败或被中断；已有内容保留" {
		t.Fatalf("abort notice leaked into the reason: %q", got.Error)
	}
}

func TestNewRoundDoesNotCarryThePreviousFailureReason(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	if got := sendRound(t, m, store, project, conversation, "fail-first", "fail:"+rateLimitReason); !strings.Contains(got.Error, "已达到 Token Plan 速率限制") {
		t.Fatalf("first failure lost its reason: %q", got.Error)
	}
	got := sendRound(t, m, store, project, conversation, "second-round", "继续处理")
	if got.RunState != "idle" {
		t.Fatalf("second round did not succeed: %q (%s)", got.RunState, got.Error)
	}
	if got.Error != "" || strings.Contains(got.Error, "已达到 Token Plan 速率限制") {
		t.Fatalf("stale failure reason survived a new round: %q", got.Error)
	}
}

func TestToolFailureIsRecordedButDoesNotFailTheRound(t *testing.T) {
	m, store, project, conversation := failureFixture(t)
	got := sendRound(t, m, store, project, conversation, "tool-failure", "toolfail:run failing command")
	if got.RunState != "idle" || got.Error != "" {
		t.Fatalf("tool failure became a round failure: %q (%q)", got.RunState, got.Error)
	}
	if len(got.Rounds) == 0 {
		t.Fatal("no round recorded")
	}
	tools := got.Rounds[len(got.Rounds)-1].Tools
	if len(tools) != 1 || !tools[0].Failed {
		t.Fatalf("tool failure not recorded truthfully: %+v", tools)
	}
}
