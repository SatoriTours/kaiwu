package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// TestInteractionFakePi is an isolated fake pi for interaction scenarios. It
// never loads the real CLI, extensions or a model.
func TestInteractionFakePi(t *testing.T) {
	if os.Getenv("KAIWU_INTERACTION_HELPER") != "1" {
		t.Skip("helper")
	}
	scenario := os.Getenv("KAIWU_INTERACTION_SCENARIO")
	capture := os.Getenv("KAIWU_INTERACTION_UI_CAPTURE")
	session, file := fixtureSession()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	mixedPrompts := 0
	emit := func(v any) { _ = encoder.Encode(v) }
	emitText := func(delta string) {
		emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": delta}})
	}
	for scanner.Scan() {
		var c map[string]any
		if json.Unmarshal(scanner.Bytes(), &c) != nil {
			os.Exit(2)
		}
		typ, _ := c["type"].(string)
		if typ == "extension_ui_response" {
			captureUILine(capture, scanner.Text())
			continue
		}
		response := map[string]any{"type": "response", "id": c["id"], "command": typ, "success": true, "data": map[string]any{
			"isStreaming": false, "isCompacting": false, "sessionId": session, "sessionFile": file, "pendingMessageCount": 0,
			"model": map[string]any{"id": fixtureArg("--model"), "provider": fixtureArg("--provider"), "contextWindow": 200000},
		}}
		if typ == "prompt" {
			response["data"] = map[string]any{"disposition": "started"}
		}
		emit(response)
		if typ != "prompt" {
			continue
		}
		writeFixtureSession(session, file)
		message, _ := c["message"].(string)
		message = strings.Split(message, "\n\n<kaiwu-reply-requirements>")[0]
		answerTurn := strings.Contains(message, "关于「")
		handled := false
		if scenario == "mixed" && !answerTurn {
			mixedPrompts++
			switch mixedPrompts {
			case 1:
				emitText("需要确认。<kaiwu-interaction>" + fakeInteractionBody() + "</kaiwu-interaction>")
			case 2:
				runFakeNative("native-confirm", scanner, capture, emit)
			default:
				emitText("plain after deferred answer")
			}
			handled = true
		}
		switch {
		case handled:
		case strings.HasPrefix(scenario, "native-") && !answerTurn:
			runFakeNative(scenario, scanner, capture, emit)
		case scenario == "ask-slow" && !answerTurn:
			emitText("需要确认。<kaiwu-interaction>")
			time.Sleep(120 * time.Millisecond)
			emitText(fakeInteractionBody())
			time.Sleep(120 * time.Millisecond)
			emitText("</kaiwu-interaction>")
			// The complete block is streamed well before the round settles:
			// this window must never be answerable.
			time.Sleep(200 * time.Millisecond)
		case scenario == "ask-once" && !answerTurn:
			emitText("需要确认。<kaiwu-interaction>" + fakeInteractionBody() + "</kaiwu-interaction>")
		case scenario == "ask-malformed" && !answerTurn:
			emitText(`格式有误。<kaiwu-interaction>{"title":"x","fields":[}</kaiwu-interaction>`)
		case scenario == "ask-legacy" && !answerTurn:
			emitText(`请回答。<kaiwu-report>{"summary":"需要输入","stage":"blocked","plan":[],"results":[],"checks":[],"blockers":[],"next":[],"question":"请提供实际环境说明"}</kaiwu-report>`)
		default:
			emitText("plain reply: " + message)
		}
		emit(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": "stop"}})
		emit(map[string]any{"type": "agent_settled"})
	}
	os.Exit(0)
}

func fakeInteractionBody() string {
	return `{"title":"确认访问环境","description":"请补充实际环境","allowCancel":true,"fields":[` +
		`{"id":"allow","type":"confirm","label":"是否允许继续","yesLabel":"允许","noLabel":"不允许"},` +
		`{"id":"network","type":"single_select","label":"网络环境","required":true,"options":[{"id":"same_lan","label":"同一局域网"},{"id":"vpn","label":"VPN"}]},` +
		`{"id":"details","type":"textarea","label":"补充说明","maxLength":4000}]}`
}

func captureUILine(path, line string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(10)
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

func runFakeNative(scenario string, scanner *bufio.Scanner, capture string, emit func(any)) {
	dialog := func(method, id string, extra map[string]any) {
		record := map[string]any{"type": "extension_ui_request", "id": id, "method": method, "title": "原生请求"}
		for k, v := range extra {
			record[k] = v
		}
		emit(record)
	}
	switch scenario {
	case "native-confirm":
		dialog("confirm", "upstream-confirm", map[string]any{"message": "All messages will be lost.", "timeout": 5000})
	case "native-select":
		dialog("select", "upstream-select", map[string]any{"options": []string{"Allow", "Block"}})
	case "native-editor":
		dialog("editor", "upstream-editor", map[string]any{"prefill": "Line 1\nLine 2"})
	case "native-notify":
		emit(map[string]any{"type": "extension_ui_request", "id": "upstream-notify", "method": "notify", "message": "info", "notifyType": "info"})
		return
	case "native-double":
		dialog("confirm", "upstream-double", map[string]any{"message": "duplicate"})
		dialog("confirm", "upstream-double", map[string]any{"message": "duplicate"})
	case "native-bad-select":
		options := make([]string, 0, 21)
		for i := 0; i < 21; i++ {
			options = append(options, fmt.Sprintf("option-%d", i))
		}
		dialog("select", "upstream-bad", map[string]any{"options": options})
	case "native-unknown":
		emit(map[string]any{"type": "extension_ui_request", "id": "upstream-unknown", "method": "future_dialog", "title": "未来对话框"})
	case "native-long-confirm":
		dialog("confirm", "upstream-long", map[string]any{"message": strings.Repeat("长", 4001)})
	case "native-long-option":
		dialog("select", "upstream-long-option", map[string]any{"options": []string{"ok", strings.Repeat("超长选项", 50)}})
	case "native-long-message":
		dialog("confirm", "upstream-long-message", map[string]any{"message": strings.Repeat("决策说明", 60)})
	default:
		return
	}
	// Block like pi does until the answer arrives on stdin. The client must
	// send a raw extension_ui_response, never a normal command.
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return
		}
		if record["type"] == "extension_ui_response" {
			captureUILine(capture, scanner.Text())
			return
		}
	}
}

func interactionEnv(t *testing.T, scenario string) (*Manager, *workbench.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{Provider: "fake", Model: "fake"})
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
	project := command(workbench.Command{OperationID: "ix-project", Type: "add_project", Name: "交互", Path: t.TempDir()}).CreatedID
	task := command(workbench.Command{OperationID: "ix-task", Type: "add_task", ProjectID: project, Title: "交互任务", Request: "实现交互"}).CreatedID
	conv := s.Snapshot().Tasks[task].ConversationID
	if e := s.Update(func(st *workbench.State) error {
		c := st.Conversations[conv]
		c.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		st.Conversations[conv] = c
		tk := st.Tasks[task]
		tk.Profile = c.Profile
		st.Tasks[task] = tk
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	exe, _ := os.Executable()
	capture := filepath.Join(root, "ui.jsonl")
	m := New(context.Background(), s, Config{Program: exe, DataDir: filepath.Join(root, "data"), BaseArgs: []string{"-test.run=TestInteractionFakePi", "--"}, ExtraEnv: []string{"KAIWU_INTERACTION_HELPER=1", "KAIWU_INTERACTION_SCENARIO=" + scenario, "KAIWU_INTERACTION_UI_CAPTURE=" + capture}})
	t.Cleanup(func() { _ = m.Close() })
	return m, s, conv, capture
}

func waitConversation(t *testing.T, s *workbench.Store, conv string, what string, pred func(workbench.Conversation) bool) workbench.Conversation {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		c := s.Snapshot().Conversations[conv]
		if pred(c) {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	c := s.Snapshot().Conversations[conv]
	t.Fatalf("%s: runState=%s error=%q interactions=%d messages=%d", what, c.RunState, c.Error, len(c.Interactions), len(c.Messages))
	return c
}

func waitInteractionIdle(t *testing.T, s *workbench.Store, conv string) {
	t.Helper()
	waitConversation(t, s, conv, "did not settle", func(c workbench.Conversation) bool { return c.RunState == "idle" })
}

func sendAndWait(t *testing.T, m *Manager, s *workbench.Store, conv, text string) {
	t.Helper()
	r, e := s.Command(workbench.Command{OperationID: "ix-send-" + text, Revision: s.Snapshot().Revision, Type: "send_message", ProjectID: s.Snapshot().Conversations[conv].ProjectID, ConversationID: conv, Text: text})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
}

func TestInteractionOnlyPublishedAtSettledBoundary(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "ask-slow")
	sendAndWait(t, m, s, conv, "slow question")
	// The complete block is already streaming; nothing may be answerable yet.
	waitConversation(t, s, conv, "streamed block became answerable", func(c workbench.Conversation) bool {
		for _, msg := range c.Messages {
			if strings.Contains(msg.Text, "</kaiwu-interaction>") && len(c.Interactions) == 0 {
				return true
			}
		}
		return false
	})
	if c := s.Snapshot().Conversations[conv]; c.RunState != "running" {
		t.Fatalf("round ended before the block was observed: %s", c.RunState)
	}
	waitInteractionIdle(t, s, conv)
	c := s.Snapshot().Conversations[conv]
	if len(c.Interactions) != 1 {
		for _, r := range c.Rounds {
			t.Logf("round %s phase=%s interactionError=%q", r.ID, r.Phase, r.InteractionError)
		}
		for _, m := range c.Messages {
			t.Logf("msg id=%s role=%s delivery=%s text=%q", m.ID, m.Role, m.Delivery, m.Text)
		}
		t.Fatalf("expected one request, got %d", len(c.Interactions))
	}
	req := c.Interactions[0]
	if req.Status != workbench.InteractionPending || req.Source != workbench.SourceAssistantQuestion || !req.Blocking || !req.AllowTextFallback || req.ID == "" {
		t.Fatalf("request %+v", req)
	}
	if req.Title != "确认访问环境" || len(req.Fields) != 3 || req.Description != "请补充实际环境" {
		t.Fatalf("field mapping %+v", req)
	}
	// The block is hidden from the visible reply, the raw text is preserved.
	var reply workbench.Message
	for _, msg := range c.Messages {
		if msg.Role == "assistant" {
			reply = msg
		}
	}
	if reply.DisplayText == nil || strings.Contains(*reply.DisplayText, "kaiwu-interaction") || !strings.Contains(*reply.DisplayText, "需要确认") {
		t.Fatalf("display text %v", reply.DisplayText)
	}
	if !strings.Contains(reply.Text, "</kaiwu-interaction>") {
		t.Fatal("raw reply not preserved")
	}
	last := c.Rounds[len(c.Rounds)-1]
	if last.Phase != "awaiting_user" {
		t.Fatalf("round phase %q", last.Phase)
	}
	// A late re-settle must not duplicate the request.
	if e := s.Update(func(st *workbench.State) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Conversations[conv].Interactions) != 1 {
		t.Fatal("duplicate request")
	}
}

func TestInteractionAnswerContinuesOriginalSession(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "ask-once")
	sendAndWait(t, m, s, conv, "first question")
	waitInteractionIdle(t, s, conv)
	before := s.Snapshot()
	req := before.Conversations[conv].Interactions[0]
	r, e := s.Command(workbench.Command{OperationID: "ix-answer", Revision: 0, Type: "answer_interaction", ProjectID: before.Conversations[conv].ProjectID, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"allow": false, "network": "same_lan"}, Comment: "已确认"})
	if e != nil {
		t.Fatal(e)
	}
	if r.CreatedID == "" {
		t.Fatal("answer created no message")
	}
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, conv)
	after := s.Snapshot()
	if after.Conversations[conv].SessionID != before.Conversations[conv].SessionID || after.Conversations[conv].SessionFile != before.Conversations[conv].SessionFile {
		t.Fatal("answer replaced the original session")
	}
	if m.Count() != 1 {
		t.Fatalf("answer created a second execution session: %d", m.Count())
	}
	stored := after.Conversations[conv].Interactions[0]
	if stored.Status != workbench.InteractionAnswered || stored.Response == nil || stored.Response.Delivery != workbench.DeliveryAccepted {
		t.Fatalf("response %+v", stored.Response)
	}
	var answer string
	for _, msg := range after.Conversations[conv].Messages {
		if msg.ID == r.CreatedID {
			answer = msg.Text
		}
	}
	if !strings.Contains(answer, "不允许") || !strings.Contains(answer, "同一局域网") || !strings.Contains(answer, "已确认") {
		t.Fatalf("answer message %q", answer)
	}
}

func TestInteractionGeneralChatDoesNotClearPendingAndMalformedFailsClosed(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "ask-once")
	sendAndWait(t, m, s, conv, "one")
	waitInteractionIdle(t, s, conv)
	first := s.Snapshot().Conversations[conv].Interactions[0]
	// A plain chat message is a new turn and never answers the open form.
	sendAndWait(t, m, s, conv, "second")
	waitInteractionIdle(t, s, conv)
	c := s.Snapshot().Conversations[conv]
	if len(c.Interactions) != 2 {
		t.Fatalf("queued requests %d", len(c.Interactions))
	}
	if c.Interactions[0].Status != workbench.InteractionPending || c.Interactions[0].ID != first.ID {
		t.Fatal("general chat cleared the pending request")
	}
	if pending, ok := workbench.PendingBlockingInteraction(s.Snapshot(), conv); !ok || pending.ID != first.ID {
		t.Fatal("pending gate lost")
	}

	m2, s2, conv2, _ := interactionEnv(t, "ask-malformed")
	sendAndWait(t, m2, s2, conv2, "malformed")
	waitInteractionIdle(t, s2, conv2)
	c2 := s2.Snapshot().Conversations[conv2]
	if len(c2.Interactions) != 0 {
		t.Fatal("malformed block produced a request")
	}
	if c2.Rounds[len(c2.Rounds)-1].InteractionError == "" {
		t.Fatal("malformed block not recorded as fail-closed")
	}
	if code, _ := s2.InteractionGate(c2.TaskID, c2.Rounds[len(c2.Rounds)-1].ID); code != "interaction_invalid" {
		t.Fatalf("delivery gate code %q", code)
	}
	var raw string
	for _, msg := range c2.Messages {
		if msg.Role == "assistant" {
			raw = msg.Text
		}
	}
	if !strings.Contains(raw, "kaiwu-interaction") {
		t.Fatal("malformed block hidden from the user")
	}
}

func TestInteractionLegacyQuestionBecomesTextFallback(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "ask-legacy")
	sendAndWait(t, m, s, conv, "legacy")
	waitInteractionIdle(t, s, conv)
	c := s.Snapshot().Conversations[conv]
	if len(c.Interactions) != 1 {
		t.Fatalf("legacy question not converted: %+v", c.Interactions)
	}
	req := c.Interactions[0]
	if len(req.Fields) != 1 || req.Fields[0].Type != workbench.FieldTextarea || !req.AllowTextFallback || req.Description != "请提供实际环境说明" {
		t.Fatalf("legacy mapping %+v", req)
	}
	r, e := s.Command(workbench.Command{OperationID: "ix-legacy-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, AnswerMode: workbench.AnswerText, Text: "Linux 局域网直连"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Send(conv, r.CreatedID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, conv)
	for _, msg := range s.Snapshot().Conversations[conv].Messages {
		if msg.ID == r.CreatedID && !strings.Contains(msg.Text, "Linux 局域网直连") {
			t.Fatalf("text fallback message %q", msg.Text)
		}
	}
}

func TestInteractionNativeConfirmUsesOriginalRequestID(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-confirm")
	sendAndWait(t, m, s, conv, "native dialog")
	c := waitConversation(t, s, conv, "native dialog not stored", func(c workbench.Conversation) bool {
		return len(c.Interactions) == 1
	})
	req := c.Interactions[0]
	if req.Source != workbench.SourceAgentUI || req.Native == nil || req.Native.Method != "confirm" || req.Native.UpstreamID != "upstream-confirm" {
		t.Fatalf("native request %+v", req.Native)
	}
	if req.AllowTextFallback || !req.AllowCancel || req.ExpiresAt == "" || len(req.Fields) != 1 || req.Fields[0].Type != workbench.FieldConfirm {
		t.Fatalf("native mapping %+v", req)
	}
	before := len(c.Messages)
	r, e := s.Command(workbench.Command{OperationID: "ix-native-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"confirmed": false}})
	if e != nil {
		t.Fatal(e)
	}
	if r.CreatedID != "" {
		t.Fatal("native answer created a chat message")
	}
	if e = m.AnswerInteraction(conv, req.ID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, conv)
	after := s.Snapshot().Conversations[conv]
	if len(after.Messages) != before {
		t.Fatalf("native answer added messages: %d -> %d", before, len(after.Messages))
	}
	stored := after.Interactions[0]
	if stored.Response == nil || stored.Response.Delivery != workbench.DeliveryAccepted || stored.Status != workbench.InteractionAnswered {
		t.Fatalf("native response %+v", stored.Response)
	}
	records := waitUICapture(t, capture, 1)
	if len(records) != 1 {
		t.Fatalf("captured %d raw responses", len(records))
	}
	if records[0]["type"] != "extension_ui_response" || records[0]["id"] != "upstream-confirm" || records[0]["confirmed"] != false {
		t.Fatalf("raw confirm %v", records[0])
	}
}

func TestInteractionNativeSelectEditorAndCancel(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-select")
	sendAndWait(t, m, s, conv, "select")
	c := waitConversation(t, s, conv, "select not stored", func(c workbench.Conversation) bool { return len(c.Interactions) == 1 })
	req := c.Interactions[0]
	if len(req.Fields[0].Options) != 2 || req.Fields[0].Options[1].ID != "o1" || req.Fields[0].Options[1].Label != "Block" {
		t.Fatalf("select mapping %+v", req.Fields)
	}
	if _, e := s.Command(workbench.Command{OperationID: "ix-select-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"value": "o1"}}); e != nil {
		t.Fatal(e)
	}
	if e := m.AnswerInteraction(conv, req.ID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, conv)
	records := waitUICapture(t, capture, 1)
	if len(records) != 1 || records[0]["value"] != "Block" || records[0]["id"] != "upstream-select" {
		t.Fatalf("raw select %v", records)
	}

	m2, s2, conv2, capture2 := interactionEnv(t, "native-editor")
	sendAndWait(t, m2, s2, conv2, "editor")
	c2 := waitConversation(t, s2, conv2, "editor not stored", func(c workbench.Conversation) bool { return len(c.Interactions) == 1 })
	req2 := c2.Interactions[0]
	if req2.Fields[0].Type != workbench.FieldTextarea || req2.Fields[0].Prefill != "Line 1\nLine 2" || !req2.Fields[0].AllowEmpty {
		t.Fatalf("editor mapping %+v", req2.Fields)
	}
	if _, e := s2.Command(workbench.Command{OperationID: "ix-editor-cancel", Revision: 0, Type: "cancel_interaction", ProjectID: c2.ProjectID, ConversationID: conv2, RequestID: req2.ID, RequestVersion: req2.Version}); e != nil {
		t.Fatal(e)
	}
	if e := m2.AnswerInteraction(conv2, req2.ID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s2, conv2)
	records2 := waitUICapture(t, capture2, 1)
	if len(records2) != 1 || records2[0]["cancelled"] != true || records2[0]["id"] != "upstream-editor" {
		t.Fatalf("raw cancel %v", records2)
	}
	if _, has := records2[0]["value"]; has {
		t.Fatal("cancel carried a value")
	}
}

func TestInteractionNativeUnsupportedAndDuplicateRequests(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-notify")
	sendAndWait(t, m, s, conv, "notify")
	waitInteractionIdle(t, s, conv)
	if len(s.Snapshot().Conversations[conv].Interactions) != 0 {
		t.Fatal("fire-and-forget notify produced a form")
	}
	time.Sleep(100 * time.Millisecond)
	if records := readUICapture(t, capture); len(records) != 0 {
		t.Fatalf("notify answered upstream: %v", records)
	}

	m2, s2, conv2, capture2 := interactionEnv(t, "native-double")
	sendAndWait(t, m2, s2, conv2, "double")
	c := waitConversation(t, s2, conv2, "duplicate not stored", func(c workbench.Conversation) bool { return len(c.Interactions) == 1 })
	time.Sleep(100 * time.Millisecond)
	if len(s2.Snapshot().Conversations[conv2].Interactions) != 1 {
		t.Fatal("duplicate upstream event stacked a second form")
	}
	req := c.Interactions[0]
	if _, e := s2.Command(workbench.Command{OperationID: "ix-double-cancel", Revision: 0, Type: "cancel_interaction", ProjectID: c.ProjectID, ConversationID: conv2, RequestID: req.ID, RequestVersion: req.Version}); e != nil {
		t.Fatal(e)
	}
	if e := m2.AnswerInteraction(conv2, req.ID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s2, conv2)
	if records := waitUICapture(t, capture2, 1); len(records) != 1 {
		t.Fatalf("duplicate responses written: %v", records)
	}
}

func TestInteractionNativeUnrepresentableSelectIsCancelledUpstream(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-bad-select")
	sendAndWait(t, m, s, conv, "bad select")
	records := waitUICapture(t, capture, 1)
	if c := s.Snapshot().Conversations[conv]; len(c.Interactions) != 0 {
		t.Fatalf("unrepresentable native request became a form: %+v", c.Interactions)
	}
	if len(records) != 1 || records[0]["cancelled"] != true || records[0]["id"] != "upstream-bad" {
		t.Fatalf("fail-closed cancel missing: %v", records)
	}
	waitInteractionIdle(t, s, conv)
}

func TestInteractionNativeExpiresOnProcessLossAndNeverResends(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-confirm")
	sendAndWait(t, m, s, conv, "native")
	c := waitConversation(t, s, conv, "native not stored", func(c workbench.Conversation) bool { return len(c.Interactions) == 1 })
	req := c.Interactions[0]
	if _, e := s.Command(workbench.Command{OperationID: "ix-lost-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"confirmed": true}}); e != nil {
		t.Fatal(e)
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	after := s.Snapshot().Conversations[conv]
	stored := after.Interactions[0]
	if stored.Status != workbench.InteractionAnswered || stored.Response == nil || stored.Response.Delivery == workbench.DeliveryAccepted {
		t.Fatalf("recorded answer lost on process loss: %+v", stored)
	}
	m2 := New(context.Background(), s, m.cfg)
	t.Cleanup(func() { _ = m2.Close() })
	if err := m2.AnswerInteraction(conv, stored.ID); err == nil {
		t.Fatal("stale native answer resent to a new process")
	}
	time.Sleep(100 * time.Millisecond)
	if records := readUICapture(t, capture); len(records) != 0 {
		t.Fatalf("stale answer written to a new process: %v", records)
	}
	// The recorded answer stays, with an explicit non-delivery state.
	latest, err := s.Interaction(conv, stored.ID)
	if err != nil || latest.Response == nil || latest.Response.Delivery != workbench.DeliveryNotSent {
		t.Fatalf("response %+v %v", latest, err)
	}
}

func waitUICapture(t *testing.T, path string, want int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		records := readUICapture(t, path)
		if len(records) >= want || time.Now().After(deadline) {
			return records
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readUICapture(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records := []map[string]any{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid captured raw response %q", scanner.Text())
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestInteractionWaitingStateReplacesResultConfirmation(t *testing.T) {
	m, task, _, _ := workspaceFixture(t)
	s := m.store
	exe, _ := os.Executable()
	m.cfg.Program = exe
	m.cfg.BaseArgs = []string{"-test.run=TestInteractionFakePi", "--"}
	m.cfg.ExtraEnv = []string{"KAIWU_INTERACTION_HELPER=1", "KAIWU_INTERACTION_SCENARIO=ask-once"}
	if e := s.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.ID, c.ProjectID, c.TaskID = task.ConversationID, task.ProjectID, task.ID
		c.SessionID = "kaiwu_" + task.ConversationID
		c.RunState = "idle"
		c.Messages = []workbench.Message{}
		c.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		st.Conversations[c.ID] = c
		tk := st.Tasks[task.ID]
		tk.Profile = c.Profile
		st.Tasks[tk.ID] = tk
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	start := command(workbench.Command{OperationID: "waiting-start", Type: "start_task", ProjectID: "p", TaskID: task.ID})
	if e := m.Send(task.ConversationID, start.CreatedID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, task.ConversationID)
	st := s.Snapshot()
	waiting := st.Tasks[task.ID]
	x := st.Executions[waiting.ExecutionID]
	lastRound := st.Conversations[task.ConversationID].Rounds[len(st.Conversations[task.ConversationID].Rounds)-1]
	if waiting.Status != "waiting_user" || x.Phase != "awaiting_user" || lastRound.Phase != "awaiting_user" {
		t.Fatalf("task not waiting for the user: %+v %+v %q", waiting.Status, x.Phase, lastRound.Phase)
	}
	if x.FinishedAt != "" || waiting.Status == "needs_confirmation" || lastRound.Report != nil {
		t.Fatal("blocking question presented as a finished result")
	}
	// Result confirmation and a second development start are blocked server-side.
	if _, e := s.Command(workbench.Command{OperationID: "waiting-confirm", Revision: s.Snapshot().Revision, Type: "confirm_result", ProjectID: "p", TaskID: task.ID, ConversationID: task.ConversationID, RoundID: lastRound.ID}); e == nil {
		t.Fatal("result confirmed while the agent waits for an answer")
	}
	if _, e := s.Command(workbench.Command{OperationID: "waiting-restart", Revision: s.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: task.ID}); e == nil {
		t.Fatal("development restarted while the agent waits for an answer")
	}
	req := st.Conversations[task.ConversationID].Interactions[0]
	answer, e := s.Command(workbench.Command{OperationID: "waiting-answer", Revision: 0, Type: "answer_interaction", ProjectID: "p", ConversationID: task.ConversationID, TaskID: task.ID, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"allow": true, "network": "vpn"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Send(task.ConversationID, answer.CreatedID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, task.ConversationID)
	st = s.Snapshot()
	x = st.Executions[waiting.ExecutionID]
	rounds := st.Conversations[task.ConversationID].Rounds
	if st.Tasks[task.ID].Status != "needs_confirmation" || x.Phase != "awaiting_review" || rounds[len(rounds)-1].Phase != "finished" {
		t.Fatalf("answered round did not become a result: %+v %+v", st.Tasks[task.ID].Status, x.Phase)
	}
	if _, e = s.Command(workbench.Command{OperationID: "waiting-confirm-2", Revision: s.Snapshot().Revision, Type: "confirm_result", ProjectID: "p", TaskID: task.ID, ConversationID: task.ConversationID, RoundID: rounds[len(rounds)-1].ID}); e != nil {
		t.Fatal("answered result still blocked:", e)
	}
}

func TestInteractionAnswerRecordedDuringRunIsFlushedAtBoundary(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "mixed")
	sendAndWait(t, m, s, conv, "first")
	waitInteractionIdle(t, s, conv)
	first := s.Snapshot().Conversations[conv]
	if len(first.Interactions) != 1 || first.Interactions[0].Status != workbench.InteractionPending {
		t.Fatalf("first question %+v", first.Interactions)
	}
	question := first.Interactions[0]
	// A general message is allowed; the fake then opens a native dialog and
	// keeps the run active.
	sendAndWait(t, m, s, conv, "second")
	c := waitConversation(t, s, conv, "native dialog not stored", func(c workbench.Conversation) bool {
		for _, r := range c.Interactions {
			if r.Source == workbench.SourceAgentUI && r.Status == workbench.InteractionPending {
				return true
			}
		}
		return false
	})
	if c.RunState != "running" {
		t.Fatalf("run state %q", c.RunState)
	}
	// Answering the ordinary question while the run is active only records it.
	beforeAnswer := len(c.Messages)
	r, e := s.Command(workbench.Command{OperationID: "mixed-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, TaskID: c.TaskID, RequestID: question.ID, RequestVersion: question.Version, Answers: map[string]any{"allow": true, "network": "same_lan"}})
	if e != nil {
		t.Fatal(e)
	}
	if r.CreatedID != "" {
		t.Fatal("answer created a turn while the session was running")
	}
	mid := s.Snapshot().Conversations[conv]
	if mid.Interactions[0].Response.Note == "" || mid.Interactions[0].Response.MessageID != "" {
		t.Fatalf("deferred answer %+v", mid.Interactions[0].Response)
	}
	if len(mid.Messages) != beforeAnswer {
		t.Fatalf("deferred answer added a message: %d -> %d", beforeAnswer, len(mid.Messages))
	}
	// Resolve the native dialog; the run settles and the boundary flush sends
	// exactly one aggregated turn with the recorded answer.
	var native workbench.InteractionRequest
	for _, req := range mid.Interactions {
		if req.Source == workbench.SourceAgentUI {
			native = req
		}
	}
	if _, e = s.Command(workbench.Command{OperationID: "mixed-native", Revision: 0, Type: "answer_interaction", ProjectID: mid.ProjectID, ConversationID: conv, TaskID: mid.TaskID, RequestID: native.ID, RequestVersion: native.Version, Answers: map[string]any{"confirmed": true}}); e != nil {
		t.Fatal(e)
	}
	if e = m.AnswerInteraction(conv, native.ID); e != nil {
		t.Fatal(e)
	}
	waitConversation(t, s, conv, "boundary flush did not send the answer", func(c workbench.Conversation) bool {
		for _, msg := range c.Messages {
			if msg.Role == "user" && strings.Contains(msg.Text, "关于「") {
				return true
			}
		}
		return false
	})
	waitInteractionIdle(t, s, conv)
	final := s.Snapshot().Conversations[conv]
	var answerTurn string
	answerMessages := 0
	for _, msg := range final.Messages {
		if msg.Role == "user" && strings.Contains(msg.Text, "关于「") {
			answerTurn = msg.Text
			answerMessages++
		}
	}
	if answerMessages != 1 || !strings.Contains(answerTurn, "同一局域网") {
		t.Fatalf("boundary flush did not send the recorded answer once: %q", answerTurn)
	}
	if final.Interactions[0].Response == nil || final.Interactions[0].Response.MessageID == "" || final.Interactions[0].Response.Delivery != workbench.DeliveryAccepted {
		t.Fatalf("flushed answer %+v", final.Interactions[0].Response)
	}
}

func TestInteractionNativeUnknownMethodIsCancelledAndVisible(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-unknown")
	sendAndWait(t, m, s, conv, "unknown method")
	records := waitUICapture(t, capture, 1)
	if len(records) != 1 || records[0]["cancelled"] != true || records[0]["id"] != "upstream-unknown" {
		t.Fatalf("unknown native method not cancelled: %v", records)
	}
	waitInteractionIdle(t, s, conv)
	c := s.Snapshot().Conversations[conv]
	if len(c.Interactions) != 0 {
		t.Fatalf("unknown method produced a form: %+v", c.Interactions)
	}
	notice := c.Rounds[len(c.Rounds)-1].InteractionNotice
	if !strings.Contains(notice, "future_dialog") || !strings.Contains(notice, "confirm/select/input/editor") {
		t.Fatalf("capability notice missing: %q", notice)
	}
}

func TestInteractionNativeOverLimitDecisionIsCancelledNotTruncated(t *testing.T) {
	for name, scenario := range map[string]string{"long-confirm": "native-long-confirm", "long-option": "native-long-option"} {
		t.Run(name, func(t *testing.T) {
			m, s, conv, capture := interactionEnv(t, scenario)
			sendAndWait(t, m, s, conv, "over limit")
			records := waitUICapture(t, capture, 1)
			if len(records) != 1 || records[0]["cancelled"] != true {
				t.Fatalf("over-limit decision not cancelled: %v", records)
			}
			waitInteractionIdle(t, s, conv)
			c := s.Snapshot().Conversations[conv]
			if len(c.Interactions) != 0 {
				t.Fatalf("truncated decision became answerable: %+v", c.Interactions[0])
			}
			if notice := c.Rounds[len(c.Rounds)-1].InteractionNotice; !strings.Contains(notice, "未展示") {
				t.Fatalf("over-limit notice missing: %q", notice)
			}
		})
	}
}

func TestInteractionNativeLongConfirmKeepsCompleteDecisionVisible(t *testing.T) {
	m, s, conv, capture := interactionEnv(t, "native-long-message")
	sendAndWait(t, m, s, conv, "long but valid")
	c := waitConversation(t, s, conv, "long confirm not stored", func(c workbench.Conversation) bool {
		return len(c.Interactions) == 1
	})
	req := c.Interactions[0]
	if req.Native == nil || req.Native.Method != "confirm" {
		t.Fatalf("native mapping %+v", req.Native)
	}
	if req.Fields[0].Label != "请确认" {
		t.Fatalf("long decision was not labelled generically: %q", req.Fields[0].Label)
	}
	full := strings.Repeat("决策说明", 60)
	if req.Description != full {
		t.Fatalf("decision content truncated: %q", req.Description)
	}
	if _, e := s.Command(workbench.Command{OperationID: "long-confirm-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, TaskID: c.TaskID, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"confirmed": true}}); e != nil {
		t.Fatal(e)
	}
	if e := m.AnswerInteraction(conv, req.ID); e != nil {
		t.Fatal(e)
	}
	waitInteractionIdle(t, s, conv)
	records := waitUICapture(t, capture, 1)
	if len(records) != 1 || records[0]["confirmed"] != true {
		t.Fatalf("long decision answer %v", records)
	}
}

func TestInteractionNativeAcceptedResponseEndsUserWait(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "native-confirm")
	sendAndWait(t, m, s, conv, "native resume boundary")
	c := waitConversation(t, s, conv, "native dialog not stored", func(c workbench.Conversation) bool {
		return len(c.Interactions) == 1
	})
	req := c.Interactions[0]
	var hits int
	var gotTask, gotConv string
	s.SetUserTurnHook(func(taskID, conversationID string) {
		hits++
		gotTask, gotConv = taskID, conversationID
	})
	if _, e := s.Command(workbench.Command{OperationID: "resume-boundary-answer", Revision: 0, Type: "answer_interaction", ProjectID: c.ProjectID, ConversationID: conv, TaskID: c.TaskID, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"confirmed": true}}); e != nil {
		t.Fatal(e)
	}
	if hits != 0 {
		t.Fatalf("recording a native answer alone must not end the wait: %d", hits)
	}
	if e := m.AnswerInteraction(conv, req.ID); e != nil {
		t.Fatal(e)
	}
	if hits != 1 || gotTask != c.TaskID || gotConv != conv {
		t.Fatalf("accepted native response did not notify the resume boundary: hits=%d task=%q conv=%q", hits, gotTask, gotConv)
	}
	waitInteractionIdle(t, s, conv)
	s.SetUserTurnHook(nil)
}

func TestInteractionNativeCancelledResponseEndsUserWait(t *testing.T) {
	m, s, conv, _ := interactionEnv(t, "native-confirm")
	sendAndWait(t, m, s, conv, "native cancel boundary")
	c := waitConversation(t, s, conv, "native dialog not stored", func(c workbench.Conversation) bool {
		return len(c.Interactions) == 1
	})
	req := c.Interactions[0]
	var hits int
	s.SetUserTurnHook(func(taskID, conversationID string) { hits++ })
	if _, e := s.Command(workbench.Command{OperationID: "cancel-boundary", Revision: 0, Type: "cancel_interaction", ProjectID: c.ProjectID, ConversationID: conv, TaskID: c.TaskID, RequestID: req.ID, RequestVersion: req.Version}); e != nil {
		t.Fatal(e)
	}
	if e := m.AnswerInteraction(conv, req.ID); e != nil {
		t.Fatal(e)
	}
	if hits != 1 {
		t.Fatalf("accepted native cancel did not end the user wait: %d", hits)
	}
	waitInteractionIdle(t, s, conv)
	s.SetUserTurnHook(nil)
}
