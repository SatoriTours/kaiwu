package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// The real provider envelope (shape taken from a captured server session, with
// synthetic identifiers) must surface the actionable reason, never the shell.
func TestFailureDetailPrefersProviderReasonOverEnvelope(t *testing.T) {
	raw := `429 {"type":"error","error":{"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)"},"request_id":"synthetic-request-id"}`
	got := safeFailureDetail(raw)
	if !strings.Contains(got, "已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)") {
		t.Fatalf("provider reason lost: %q", got)
	}
	if !strings.Contains(got, "429") {
		t.Fatalf("http status lost: %q", got)
	}
	for _, leak := range []string{"synthetic-request-id", "rate_limit_error", "{", "}"} {
		if strings.Contains(got, leak) {
			t.Fatalf("raw envelope stored (%q): %q", leak, got)
		}
	}
}

func TestFailureDetailFallsBackWhenEnvelopeHasNoReason(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		`{"type":"error","error":{"type":"unknown"}}`,
	} {
		if got := safeFailureDetail(raw); got != "" {
			t.Fatalf("expected no detail for %q, got %q", raw, got)
		}
	}
}

func TestFailureDetailKeepsPlainTextSingleLineAndBounded(t *testing.T) {
	if got := safeFailureDetail("Connection\x00 error.\n  Retry\tlater.  "); got != "Connection error. Retry later." {
		t.Fatalf("plain text not normalized: %q", got)
	}
	long := strings.Repeat("错", maxFailureDetailRunes*3)
	got := safeFailureDetail(long)
	if runes := []rune(got); len(runes) > maxFailureDetailRunes+1 {
		t.Fatalf("detail not bounded: %d runes", len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncation not marked: %q", got)
	}
}

func TestFailureDetailMasksCredentialLikeText(t *testing.T) {
	raw := `401 {"type":"error","error":{"type":"authentication_error","message":"Invalid key sk-live-SYNTHETICMARKER123 for api_key=abcd1234efgh; retry with Bearer SYNTHETICBEARER0123"}}`
	got := safeFailureDetail(raw)
	if got == "" {
		t.Fatal("authentication reason dropped")
	}
	for _, secret := range []string{"SYNTHETICMARKER123", "abcd1234efgh", "SYNTHETICBEARER0123"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential text stored (%q): %q", secret, got)
		}
	}
	if !strings.Contains(got, "Invalid key") {
		t.Fatalf("reason lost while masking: %q", got)
	}
}

func TestFailureMessageKeepsExistingFallbackWithoutReason(t *testing.T) {
	if got := failureMessage(""); got != "模型调用失败或被中断；已有内容保留" {
		t.Fatalf("fallback changed: %q", got)
	}
	got := failureMessage("已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)")
	if !strings.HasPrefix(got, "模型调用失败；已有内容保留：") || !strings.Contains(got, "已达到 Token Plan 速率限制") {
		t.Fatalf("reason not appended to failure text: %q", got)
	}
}

// The bounded reason must reach both existing error channels, so the task card
// and the conversation show the same factual cause.
func TestFailureReasonReachesConversationAndExecutionError(t *testing.T) {
	root := t.TempDir()
	store, err := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Update(func(st *workbench.State) error {
		st.Tasks["task"] = workbench.Task{ID: "task", ProjectID: "p", ConversationID: "conv", Status: "running", ExecutionID: "exec"}
		st.Conversations["conv"] = workbench.Conversation{ID: "conv", ProjectID: "p", TaskID: "task", RunState: "running"}
		st.Executions["exec"] = workbench.Execution{ID: "exec", TaskID: "task", ConversationID: "conv", Phase: "running"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), store, Config{DataDir: root})
	defer m.Close()
	message := failureMessage(safeFailureDetail(`429 {"type":"error","error":{"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)"}}`))
	m.failed("conv", message, false)
	snap := store.Snapshot()
	if snap.Conversations["conv"].Error != message {
		t.Fatalf("conversation error lost the reason: %q", snap.Conversations["conv"].Error)
	}
	if snap.Executions["exec"].Error != message {
		t.Fatalf("execution error diverged: %q", snap.Executions["exec"].Error)
	}
	if snap.Executions["exec"].Phase != "failed" {
		t.Fatalf("execution phase changed: %q", snap.Executions["exec"].Phase)
	}
}
