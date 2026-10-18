package workbench

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// interactionStore opens a store and returns it with one project, one task and
// their two conversations: the project conversation and the task conversation.
func interactionStore(t *testing.T) (*Store, string, string, string, string) {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	cmd := func(c Command) Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := cmd(Command{OperationID: "ix-project", Type: "add_project", Name: "交互", Path: t.TempDir()}).CreatedID
	task := cmd(Command{OperationID: "ix-task", Type: "add_task", ProjectID: project, Title: "交互任务", Request: "实现交互"}).CreatedID
	st := s.Snapshot()
	conv := st.Tasks[task].ConversationID
	return s, project, st.Tasks[task].TopicID, task, conv
}

func interactionDefinition() InteractionDefinition {
	min := 1
	max := 4
	lo := 0.0
	hi := 10.0
	return InteractionDefinition{
		Title:       "确认访问环境",
		Description: "请补充实际环境",
		AllowCancel: true,
		Fields: []InteractionField{
			{ID: "allow", Type: FieldConfirm, Label: "是否允许继续", Required: true, YesLabel: "允许", NoLabel: "不允许"},
			{ID: "network", Type: FieldSingleSelect, Label: "网络环境", Required: true, Options: []InteractionOption{{ID: "same_lan", Label: "同一局域网"}, {ID: "vpn", Label: "VPN", Recommended: true}}, AllowOther: true},
			{ID: "checks", Type: FieldMultiSelect, Label: "已检查", Required: true, Options: []InteractionOption{{ID: "api", Label: "接口"}, {ID: "ui", Label: "界面"}, {ID: "mobile", Label: "手机"}}, MinSelections: &min, MaxSelections: &max, AllowOther: true},
			{ID: "note", Type: FieldText, Label: "补充", MaxLength: intPtr(120)},
			{ID: "details", Type: FieldTextarea, Label: "详细说明", MaxLength: intPtr(4000)},
			{ID: "port", Type: FieldNumber, Label: "端口", Required: true, Integer: true, Min: &lo, Max: &hi, Unit: "秒"},
		},
	}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func seedInteraction(t *testing.T, s *Store, project, task, conv string, req InteractionRequest) InteractionRequest {
	t.Helper()
	if req.ID == "" {
		req.ID = "ix_fixture"
	}
	req.SchemaVersion = InteractionSchemaVersion
	if req.Version == 0 {
		req.Version = 1
	}
	req.ProjectID = project
	req.TaskID = task
	req.ConversationID = conv
	if req.RoundID == "" {
		req.RoundID = "round-1"
	}
	if req.Source == "" {
		req.Source = SourceAssistantQuestion
	}
	if req.Status == "" {
		req.Status = InteractionPending
	}
	if req.CreatedAt == "" {
		req.CreatedAt = "2026-10-18T10:00:00Z"
	}
	if req.Title == "" {
		req.Title = "需要确认"
	}
	if len(req.Fields) == 0 {
		req.Fields = interactionDefinition().Fields
	}
	req.Blocking = true
	if req.DedupeKey == "" {
		req.DedupeKey = req.Source + ":" + req.RoundID
	}
	var out InteractionRequest
	if e := s.Update(func(st *State) error {
		created, _, err := PublishInteraction(st, req)
		out = created
		return err
	}); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestInteractionParseAcceptsSixFieldTypesAndStripsBlock(t *testing.T) {
	def := interactionDefinition()
	body, e := json.Marshal(map[string]any{
		"title":       def.Title,
		"description": def.Description,
		"allowCancel": true,
		"fields":      def.Fields,
	})
	if e != nil {
		t.Fatal(e)
	}
	text := "先给结论。\n<kaiwu-interaction>" + string(body) + "</kaiwu-interaction>"
	block, visible := ParseInteractionBlock(text)
	if block == nil {
		t.Fatalf("structured block rejected: %s", visible)
	}
	if strings.TrimSpace(visible) != "先给结论。" {
		t.Fatalf("visible text %q", visible)
	}
	if block.Definition.Title != def.Title || !block.Definition.AllowCancel || len(block.Definition.Fields) != 6 {
		t.Fatalf("definition %+v", block.Definition)
	}
	types := map[string]string{}
	for _, f := range block.Definition.Fields {
		types[f.ID] = f.Type
	}
	for id, want := range map[string]string{"allow": FieldConfirm, "network": FieldSingleSelect, "checks": FieldMultiSelect, "note": FieldText, "details": FieldTextarea, "port": FieldNumber} {
		if types[id] != want {
			t.Fatalf("field %s type %q", id, types[id])
		}
	}
	if block.Fingerprint == "" || !strings.HasPrefix(block.Fingerprint, "ixf_") {
		t.Fatalf("fingerprint %q", block.Fingerprint)
	}
	// Same block re-parsed must dedupe; a changed block must not.
	again, _ := ParseInteractionBlock(text)
	if again.Fingerprint != block.Fingerprint {
		t.Fatal("fingerprint not deterministic")
	}
	changed, _ := ParseInteractionBlock(strings.Replace(text, "确认访问环境", "确认部署环境", 1))
	if changed.Fingerprint == block.Fingerprint {
		t.Fatal("changed content kept fingerprint")
	}
}

func TestInteractionParseRejectsMalformedAndOversize(t *testing.T) {
	valid := `{"title":"请选择","fields":[{"id":"choice","type":"single_select","label":"选项","required":true,"options":[{"id":"a","label":"A"}]}]}`
	if b, _ := ParseInteractionBlock("<kaiwu-interaction>" + valid + "</kaiwu-interaction>"); b == nil {
		t.Fatal("baseline rejected")
	}
	thirteen := make([]map[string]any, 0, 13)
	for i := 0; i < 13; i++ {
		thirteen = append(thirteen, map[string]any{"id": fmt.Sprintf("f%d", i), "type": "text", "label": "字段"})
	}
	tooManyOptions := make([]map[string]any, 0, 21)
	for i := 0; i < 21; i++ {
		tooManyOptions = append(tooManyOptions, map[string]any{"id": fmt.Sprintf("o%d", i), "label": "选项"})
	}
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	for name, raw := range map[string]string{
		"unknown-field":    `{"title":"x","fields":[{"id":"a","type":"text","label":"l"}],"projectId":"spoof"}`,
		"unknown-fieldkey": `{"title":"x","fields":[{"id":"a","type":"text","label":"l","command":"rm -rf /"}]}`,
		"no-fields":        `{"title":"x","fields":[]}`,
		"duplicate-field":  `{"title":"x","fields":[{"id":"a","type":"text","label":"l"},{"id":"a","type":"text","label":"m"}]}`,
		"bad-type":         `{"title":"x","fields":[{"id":"a","type":"script","label":"l"}]}`,
		"bad-field-id":     `{"title":"x","fields":[{"id":"A B","type":"text","label":"l"}]}`,
		"select-no-option": `{"title":"x","fields":[{"id":"a","type":"single_select","label":"l","options":[]}]}`,
		"text-with-option": `{"title":"x","fields":[{"id":"a","type":"text","label":"l","options":[{"id":"o","label":"O"}]}]}`,
		"duplicate-option": `{"title":"x","fields":[{"id":"a","type":"single_select","label":"l","options":[{"id":"o","label":"O"},{"id":"o","label":"P"}]}]}`,
		"bad-number-min":   `{"title":"x","fields":[{"id":"a","type":"number","label":"l","min":9,"max":1}]}`,
		"too-many-fields":  `{"title":"x","fields":` + encode(thirteen) + `}`,
		"too-many-options": `{"title":"x","fields":[{"id":"a","type":"multi_select","label":"l","options":` + encode(tooManyOptions) + `}]}`,
		"long-title":       `{"title":"` + strings.Repeat("题", 161) + `","fields":[{"id":"a","type":"text","label":"l"}]}`,
		"oversize":         `{"title":"x","description":"` + strings.Repeat("d", 33<<10) + `","fields":[{"id":"a","type":"text","label":"l"}]}`,
		"half":             `{"title":"x","fields":[{"id":"a","type":"text"`,
		"trailing":         valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			text := "原文<kaiwu-interaction>" + raw + "</kaiwu-interaction>"
			block, visible := ParseInteractionBlock(text)
			if block != nil || visible != text {
				t.Fatalf("accepted malformed block: %+v", block)
			}
		})
	}
}

func TestInteractionValidateAnswersEnforcesTypes(t *testing.T) {
	def := interactionDefinition()
	req := InteractionRequest{ID: "ix", Title: "问题", Fields: def.Fields, Status: InteractionPending}
	base := func() Submission {
		return Submission{Mode: AnswerForm, Answers: map[string]any{"allow": false, "network": "same_lan", "checks": []any{"api", "ui"}, "port": float64(0)}}
	}
	if out, e := ValidateInteractionSubmission(req, base()); e != nil || out.Answers["allow"] != false || out.Answers["port"] != float64(0) {
		t.Fatalf("false/0 rejected: %v %v", out, e)
	}
	// "other" replaces an option and counts toward selection minimums.
	alt := base()
	delete(alt.Answers, "network")
	alt.Others = map[string]string{"network": "独立专线", "checks": "回归脚本"}
	if _, e := ValidateInteractionSubmission(req, alt); e != nil {
		t.Fatal("other rejected", e)
	}
	for name, mutate := range map[string]func(*Submission){
		"missing-required": func(s *Submission) { delete(s.Answers, "allow") },
		"confirm-not-bool": func(s *Submission) { s.Answers["allow"] = "yes" },
		"unknown-answer":   func(s *Submission) { s.Answers["admin"] = true },
		"unknown-option":   func(s *Submission) { s.Answers["network"] = "guess" },
		"too-few":          func(s *Submission) { s.Answers["checks"] = []any{} },
		"too-many":         func(s *Submission) { s.Answers["checks"] = []any{"api", "ui", "mobile", "other4", "other5"} },
		"duplicate-choice": func(s *Submission) { s.Answers["checks"] = []any{"api", "api"} },
		"fraction":         func(s *Submission) { s.Answers["port"] = 1.5 },
		"out-of-range":     func(s *Submission) { s.Answers["port"] = 11.0 },
		"not-number":       func(s *Submission) { s.Answers["port"] = "3" },
		"note-too-long":    func(s *Submission) { s.Answers["note"] = strings.Repeat("字", 121) },
		"comment-too-long": func(s *Submission) { s.Comment = strings.Repeat("注", 2001) },
		"other-not-allowed": func(s *Submission) {
			delete(s.Answers, "note")
			s.Others = map[string]string{"note": "x"}
		},
		"option-and-other": func(s *Submission) { s.Others = map[string]string{"network": "x"} },
		"text-mode-without-fallback": func(s *Submission) {
			s.Mode = AnswerText
			s.Text = "直接说"
			s.Answers = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := base()
			mutate(&in)
			if _, e := ValidateInteractionSubmission(req, in); e == nil {
				t.Fatal("invalid submission accepted")
			}
		})
	}
	withFallback := req
	withFallback.AllowTextFallback = true
	if _, e := ValidateInteractionSubmission(withFallback, Submission{Mode: AnswerText, Text: "  直接回答  "}); e != nil {
		t.Fatal("text fallback rejected", e)
	}
	if _, e := ValidateInteractionSubmission(withFallback, Submission{Mode: AnswerText, Text: "  "}); e == nil {
		t.Fatal("blank text fallback accepted")
	}
	// Required native input may legitimately be the empty string.
	blank := InteractionRequest{Fields: []InteractionField{{ID: "value", Type: FieldText, Label: "输入", Required: true, AllowEmpty: true}}}
	if _, e := ValidateInteractionSubmission(blank, Submission{Mode: AnswerForm, Answers: map[string]any{"value": ""}}); e != nil {
		t.Fatal("empty native input rejected", e)
	}
	// Total payload cap is enforced, not only per field.
	huge := InteractionRequest{}
	for i := 0; i < 9; i++ {
		huge.Fields = append(huge.Fields, InteractionField{ID: fmt.Sprintf("v%d", i), Type: FieldTextarea, Label: "长", MaxLength: intPtr(5000)})
	}
	big := map[string]any{}
	for i := 0; i < 9; i++ {
		big[fmt.Sprintf("v%d", i)] = strings.Repeat("x", 4000)
	}
	if _, e := ValidateInteractionSubmission(huge, Submission{Mode: AnswerForm, Answers: big}); e == nil {
		t.Fatal("32KiB payload cap missing")
	}
}

func TestInteractionAnswerPersistsIdempotentlyWithRequestVersion(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	req := seedInteraction(t, s, project, task, conv, InteractionRequest{})

	answer := Command{OperationID: "ix-answer", Type: "answer_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: req.ID, RequestVersion: req.Version, Answers: map[string]any{"allow": false, "network": "same_lan", "checks": []any{"api"}, "port": float64(0)}, Comment: "补充说明"}
	// A stale global revision must not block a requestVersion-bound answer.
	answer.Revision = 0
	r, e := s.Command(answer)
	if e != nil {
		t.Fatal(e)
	}
	if r.InteractionID != req.ID || r.InteractionVersion != 1 || r.Delivery != DeliveryRecorded || r.CreatedID == "" || r.Type != "answer_interaction" {
		t.Fatalf("receipt %+v", r)
	}
	st := s.Snapshot()
	stored := st.Conversations[conv].Interactions[0]
	if stored.Status != InteractionAnswered || stored.Response == nil || stored.Response.MessageID != r.CreatedID || stored.Response.Answers["allow"] != false {
		t.Fatalf("stored %+v", stored)
	}
	if st.Conversations[conv].RunState != "starting" {
		t.Fatal("answer did not start the next turn")
	}
	msg := st.Conversations[conv].Messages[len(st.Conversations[conv].Messages)-1]
	if msg.Role != "user" || !strings.Contains(msg.Text, "不允许") || !strings.Contains(msg.Text, "端口") || !strings.Contains(msg.Text, "补充说明") {
		t.Fatalf("composed message %q", msg.Text)
	}
	// Replay returns the original receipt and never duplicates the message.
	replay, e := s.Command(answer)
	if e != nil || !replay.Replay || replay.CreatedID != r.CreatedID || len(s.Snapshot().Conversations[conv].Messages) != 1 {
		t.Fatalf("replay %+v %v", replay, e)
	}
	// Same operation key with different content conflicts.
	other := answer
	other.Comment = "不同内容"
	other.Revision = s.Snapshot().Revision
	if _, e = s.Command(other); e == nil {
		t.Fatal("operation reuse accepted")
	}
	// A second, different operation cannot overwrite the first answer.
	second := answer
	second.OperationID = "ix-answer-2"
	second.Revision = s.Snapshot().Revision
	second.Answers = map[string]any{"allow": true, "network": "vpn", "checks": []any{"api"}, "port": float64(1)}
	if _, e = s.Command(second); e == nil {
		t.Fatal("second answer overwrote the first")
	}
	// Wrong request version, foreign project and unknown request are refused.
	stale := second
	stale.OperationID = "ix-answer-3"
	stale.RequestVersion = 0
	if _, e = s.Command(stale); e == nil {
		t.Fatal("wrong request version accepted")
	}
	foreign := stale
	foreign.OperationID = "ix-answer-4"
	foreign.RequestVersion = req.Version
	foreign.ProjectID = "other"
	if _, e = s.Command(foreign); e == nil {
		t.Fatal("foreign project accepted")
	}
}

func TestInteractionCancelAndRetryRules(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	req := seedInteraction(t, s, project, task, conv, InteractionRequest{AllowCancel: true})
	cancel := Command{OperationID: "ix-cancel", Type: "cancel_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: req.ID, RequestVersion: 1, Revision: 0}
	if _, e := s.Command(cancel); e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	got := st.Conversations[conv].Interactions[0]
	if got.Status != InteractionCancelled || got.Response == nil || got.Response.Mode != AnswerCancel {
		t.Fatalf("cancel %+v", got)
	}
	if len(st.Conversations[conv].Messages) != 0 {
		t.Fatal("cancel sent a message")
	}
	if _, e := s.Command(cancel); e != nil {
		t.Fatal("cancel replay failed", e)
	}

	// Ordinary answer recorded as not_sent may be retried with the same message.
	s2, p2, _, t2, c2 := interactionStore(t)
	req2 := seedInteraction(t, s2, p2, t2, c2, InteractionRequest{ID: "ix_retry"})
	r, e := s2.Command(Command{OperationID: "ix-retry-answer", Type: "answer_interaction", ProjectID: p2, TaskID: t2, ConversationID: c2, RequestID: req2.ID, RequestVersion: 1, Answers: map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s2.Update(func(st *State) error {
		c := st.Conversations[c2]
		c.RunState = "failed"
		for i := range c.Messages {
			if c.Messages[i].ID == r.CreatedID {
				c.Messages[i].Delivery = "not_sent"
			}
		}
		c.Interactions[0].Response.Delivery = DeliveryNotSent
		st.Conversations[c2] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	retry := Command{OperationID: "ix-retry", Type: "retry_interaction_answer", ProjectID: p2, TaskID: t2, ConversationID: c2, RequestID: req2.ID, RequestVersion: 1, Revision: 0}
	rr, e := s2.Command(retry)
	if e != nil {
		t.Fatal("retry of not_sent failed", e)
	}
	if rr.CreatedID != r.CreatedID || rr.Delivery != DeliveryRecorded || s2.Snapshot().Conversations[c2].RunState != "starting" {
		t.Fatalf("retry receipt %+v", rr)
	}
	// An uncertain delivery must never be retried automatically.
	if e = s2.Update(func(st *State) error {
		c := st.Conversations[c2]
		c.RunState = "interrupted"
		c.Interactions[0].Response.Delivery = DeliveryUncertain
		st.Conversations[c2] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	uncertain := retry
	uncertain.OperationID = "ix-retry-2"
	if _, e = s2.Command(uncertain); e == nil {
		t.Fatal("uncertain delivery was retried")
	}
	// Cancel is refused when the source forbids it.
	s3, p3, _, t3, c3 := interactionStore(t)
	seedInteraction(t, s3, p3, t3, c3, InteractionRequest{ID: "ix_no_cancel", AllowCancel: false})
	if _, e = s3.Command(Command{OperationID: "ix-cancel-2", Type: "cancel_interaction", ProjectID: p3, TaskID: t3, ConversationID: c3, RequestID: "ix_no_cancel", RequestVersion: 1}); e == nil {
		t.Fatal("cancel of non-cancellable request accepted")
	}
}

func TestInteractionDedupeSupersedeAndHistoryCompat(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	first := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_one", DedupeKey: "assistant_question:round-1"})
	// Same source and content re-published at the same boundary is a no-op.
	if e := s.Update(func(st *State) error {
		created, ok, err := PublishInteraction(st, InteractionRequest{ID: "ix_one_again", SchemaVersion: 1, Version: 1, ProjectID: project, TaskID: task, ConversationID: conv, RoundID: "round-1", Source: SourceAssistantQuestion, Title: first.Title, Fields: first.Fields, Blocking: true, Status: InteractionPending, CreatedAt: first.CreatedAt, DedupeKey: "assistant_question:round-1"})
		if err != nil {
			return err
		}
		if ok || created.ID != first.ID {
			return fmt.Errorf("duplicate %+v", created)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// Changed content for the same boundary supersedes the old pending request.
	changed := first
	changed.Title = "确认新的环境"
	changed.DedupeKey = "assistant_question:round-1"
	if e := s.Update(func(st *State) error {
		created, ok, err := PublishInteraction(st, InteractionRequest{ID: "ix_two", SchemaVersion: 1, Version: 1, ProjectID: project, TaskID: task, ConversationID: conv, RoundID: "round-1", Source: SourceAssistantQuestion, Title: changed.Title, Fields: changed.Fields, Blocking: true, Status: InteractionPending, CreatedAt: first.CreatedAt, DedupeKey: changed.DedupeKey})
		if err != nil {
			return err
		}
		if !ok || created.Version != 2 || created.ID != "ix_two" {
			return fmt.Errorf("not published as version 2: %+v", created)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	old := st.Conversations[conv].Interactions[0]
	if old.Status != InteractionSuperseded || old.SupersededBy != "ix_two" {
		t.Fatalf("old %+v", old)
	}
	if _, ok := PendingBlockingInteraction(st, conv); !ok {
		t.Fatal("new request missing")
	}
	// Superseded requests cannot be answered.
	if _, e := s.Command(Command{OperationID: "ix-old-answer", Type: "answer_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: old.ID, RequestVersion: 1, Answers: map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}}); e == nil {
		t.Fatal("superseded request answered")
	}
}

func TestInteractionRestartKeepsQuestionExpiresNativeAndIgnoresLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p", Path: t.TempDir()}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", RunState: "idle", Messages: []Message{}, Rounds: []Round{{ID: "r1", Phase: "awaiting_user", Report: &Report{Question: "旧的报告问题"}}}}
		return nil
	})
	seedInteraction(t, s, "p", "", "c", InteractionRequest{ID: "ix_question", DedupeKey: "assistant_question:r1", RoundID: "r1"})
	seedInteraction(t, s, "p", "", "c", InteractionRequest{ID: "ix_native_pending", DedupeKey: "agent_ui:run1:u1", RoundID: "r1", Source: SourceAgentUI, Native: &InteractionNative{Method: "confirm", UpstreamID: "u1", RunID: "run1", Generation: 1}})
	seedInteraction(t, s, "p", "", "c", InteractionRequest{ID: "ix_native_answered", DedupeKey: "agent_ui:run1:u2", RoundID: "r1", Source: SourceAgentUI, Status: InteractionAnswered, Response: &InteractionResponse{Mode: AnswerForm, Delivery: DeliveryRecorded, AnsweredAt: "2026-10-18T10:01:00Z"}, Native: &InteractionNative{Method: "input", UpstreamID: "u2", RunID: "run1", Generation: 1}})
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s2, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	st := s2.Snapshot()
	byID := map[string]InteractionRequest{}
	for _, r := range st.Conversations["c"].Interactions {
		byID[r.ID] = r
	}
	if len(byID) != 3 {
		t.Fatalf("restart invented requests: %+v", byID)
	}
	if byID["ix_question"].Status != InteractionPending {
		t.Fatal("ordinary question lost on restart")
	}
	if byID["ix_native_pending"].Status != InteractionExpired || byID["ix_native_pending"].StatusReason == "" {
		t.Fatalf("native pending survived restart: %+v", byID["ix_native_pending"])
	}
	if byID["ix_native_answered"].Response.Delivery != DeliveryUncertain {
		t.Fatalf("native recorded delivery not marked uncertain: %+v", byID["ix_native_answered"].Response)
	}
	// legacy report.question alone never becomes a pending request.
	if _, e = s2.Command(Command{OperationID: "ix-legacy", Revision: s2.Snapshot().Revision, Type: "add_task", ProjectID: "p", Title: "旧", Request: "旧问题"}); e != nil {
		t.Fatal(e)
	}
	newTask := ""
	for id, tk := range s2.Snapshot().Tasks {
		if tk.Title == "旧" {
			newTask = tk.ConversationID
			_ = id
		}
	}
	if newTask == "" || len(s2.Snapshot().Conversations[newTask].Interactions) != 0 {
		t.Fatalf("legacy question materialized: %+v", s2.Snapshot().Conversations[newTask].Interactions)
	}
}

func TestInteractionGatesSensitiveCommands(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	// Make the task look like a finished development result.
	if e := s.Update(func(st *State) error {
		tk := st.Tasks[task]
		tk.Status = "needs_confirmation"
		tk.ExecutionID = "x1"
		tk.Profile = Profile{Provider: "fake", Model: "fake"}
		st.Tasks[task] = tk
		c := st.Conversations[conv]
		c.RunState = "idle"
		c.Profile = Profile{Provider: "fake", Model: "fake"}
		c.Rounds = []Round{{ID: "round-1", Phase: "finished"}}
		st.Conversations[conv] = c
		st.Executions["x1"] = Execution{ID: "x1", TaskID: task, ConversationID: conv, Phase: "awaiting_review", FinishedAt: "2026-10-18T10:00:00Z"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	seedInteraction(t, s, project, task, conv, InteractionRequest{})
	if code, _ := InteractionGate(s.Snapshot(), task, "round-1"); code != "interaction_pending" {
		t.Fatalf("gate code %q", code)
	}
	confirm := Command{OperationID: "ix-confirm", Revision: s.Snapshot().Revision, Type: "confirm_result", ProjectID: project, TaskID: task, ConversationID: conv, RoundID: "round-1"}
	if _, e := s.Command(confirm); e == nil || !strings.Contains(e.Error(), "交互问题") {
		t.Fatalf("result confirmation gate not explicit: %v", e)
	}
	start := Command{OperationID: "ix-start", Revision: s.Snapshot().Revision, Type: "start_task", ProjectID: project, TaskID: task, ConversationID: conv}
	if _, e := s.Command(start); e == nil {
		t.Fatal("started development with a pending blocking question")
	}
	preview := MergePreview{ID: "mp", ProjectID: project, TaskID: task, CanMerge: true}
	merge := MergeRequest{OperationID: "ix-merge", ProjectID: project, TaskID: task, PreviewID: "mp", CommitMessage: "合并"}
	if _, _, e := s.BeginMerge(merge, preview); e == nil {
		t.Fatal("started merge with a pending blocking question")
	}
	// A malformed interaction on the cycle's round blocks automated delivery.
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conv]
		c.Interactions = nil
		c.Rounds[0].InteractionError = "本轮交互结构无效"
		st.Conversations[conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if code, _ := InteractionGate(s.Snapshot(), task, "round-1"); code != "interaction_invalid" {
		t.Fatalf("malformed gate code %q", code)
	}
	// Answering clears the block, so the same commands are allowed again.
	req2 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_recover"})
	if _, e := s.Command(Command{OperationID: "ix-recover-answer", Type: "answer_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: req2.ID, RequestVersion: 1, Answers: map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}}); e != nil {
		t.Fatal(e)
	}
	if _, ok := PendingBlockingInteraction(s.Snapshot(), conv); ok {
		t.Fatal("answered request still pending")
	}
	if code, _ := InteractionGate(s.Snapshot(), task, "round-1"); code != "interaction_invalid" {
		t.Fatalf("malformed round gate cleared by unrelated answer: %q", code)
	}
}

func TestInteractionReceiptEnvelopeFlattens(t *testing.T) {
	req := InteractionRequest{ID: "ix_env", Version: 1, Title: "标题", Fields: []InteractionField{{ID: "why", Type: FieldText, Label: "原因"}}, Status: InteractionPending}
	env := InteractionReceipt{Receipt: Receipt{Type: "answer_interaction", ConversationID: "c", InteractionID: req.ID, InteractionVersion: 1, Delivery: DeliveryRecorded}, Interaction: &req}
	b, e := json.Marshal(env)
	if e != nil {
		t.Fatal(e)
	}
	var decoded map[string]any
	if e = json.Unmarshal(b, &decoded); e != nil {
		t.Fatal(e)
	}
	if decoded["type"] != "answer_interaction" || decoded["interactionId"] != req.ID || decoded["delivery"] != DeliveryRecorded {
		t.Fatalf("receipt not flattened: %s", b)
	}
	interaction, ok := decoded["interaction"].(map[string]any)
	if !ok || interaction["id"] != req.ID || interaction["title"] != "标题" {
		t.Fatalf("interaction missing: %s", b)
	}
}

func TestInteractionGatesDeliveryAcceptanceAndResume(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	start := startDelivery(t, s, project, task)
	d, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	awaited := awaitingDelivery(t, s, d, "candidate-sha", "candidate-tree", start.CreatedID, "accept-token")
	conv := s.Snapshot().Tasks[task].ConversationID
	req := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_delivery", RoundID: start.CreatedID})
	if _, e := s.AcceptDelivery("accept-blocked", awaited.ID, awaited.AcceptanceToken, awaited.Version); e == nil {
		t.Fatal("delivery accepted with a pending blocking question")
	}
	if _, e := s.ControlDelivery("resume-blocked", awaited.ID, "resume", "", awaited.Version); e == nil {
		t.Fatal("delivery resumed with a pending blocking question")
	}
	// Pause stays available: it cannot advance work.
	if _, e := s.ControlDelivery("pause-ok", awaited.ID, "pause", "", awaited.Version); e != nil {
		t.Fatal("pause blocked:", e)
	}
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conv]
		c.RunState = "idle"
		st.Conversations[conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Command(Command{OperationID: "ix-delivery-answer", Revision: 0, Type: "answer_interaction", ProjectID: project, ConversationID: conv, RequestID: req.ID, RequestVersion: 1, Answers: map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}}); e != nil {
		t.Fatal(e)
	}
	paused, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	if paused.Phase != "paused" {
		t.Fatalf("paused phase %q", paused.Phase)
	}
	if _, e := s.ControlDelivery("resume-open", paused.ID, "resume", "", paused.Version); e != nil {
		t.Fatal("resume still blocked after the answer:", e)
	}
	current, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	awaitedAgain := awaitingDelivery(t, s, current, "candidate-sha", "candidate-tree", start.CreatedID, "accept-token-2")
	if _, e := s.AcceptDelivery("accept-open", awaitedAgain.ID, awaitedAgain.AcceptanceToken, awaitedAgain.Version); e != nil {
		t.Fatal("acceptance still blocked after the answer:", e)
	}
}

func TestInteractionNativeTimeoutExpiresBeforeAnswer(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	req := seedInteraction(t, s, project, task, conv, InteractionRequest{
		ID:        "ix_timeout",
		Source:    SourceAgentUI,
		ExpiresAt: "2020-01-01T00:00:00Z",
		Native:    &InteractionNative{Method: "confirm", UpstreamID: "upstream-timeout", RunID: "run_timeout", Generation: 1},
	})
	// A request past its upstream deadline no longer blocks dependent work.
	if _, ok := PendingBlockingInteraction(s.Snapshot(), conv); ok {
		t.Fatal("expired native request still blocks")
	}
	if _, e := s.Command(Command{OperationID: "ix-timeout-answer", Revision: 0, Type: "answer_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: req.ID, RequestVersion: 1, Answers: map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}}); e == nil {
		t.Fatal("expired native request was answered")
	}
	stored := s.Snapshot().Conversations[conv].Interactions[0]
	if stored.Status != InteractionExpired || stored.StatusReason == "" {
		t.Fatalf("timeout not materialized: %+v", stored)
	}
}

func answerCommand(req InteractionRequest, op string, answers map[string]any) Command {
	return Command{OperationID: op, Revision: 0, Type: "answer_interaction", ProjectID: req.ProjectID, ConversationID: req.ConversationID, TaskID: req.TaskID, RequestID: req.ID, RequestVersion: req.Version, Answers: answers}
}

func TestInteractionBatchAnswersDispatchOnceAfterAllAnswered(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	q1 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_batch1", RoundID: "r1", DedupeKey: "assistant_question:r1"})
	q2 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_batch2", RoundID: "r2", DedupeKey: "assistant_question:r2"})
	answers := map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(3)}
	first, e := s.Command(answerCommand(q1, "batch-answer-1", answers))
	if e != nil {
		t.Fatal(e)
	}
	if first.CreatedID != "" {
		t.Fatal("first answer dispatched before the queue was answered")
	}
	st := s.Snapshot()
	if len(st.Conversations[conv].Messages) != 0 {
		t.Fatal("partial batch created a user turn")
	}
	firstReq := st.Conversations[conv].Interactions[0]
	if firstReq.Status != InteractionAnswered || firstReq.Response == nil || firstReq.Response.MessageID != "" || !strings.Contains(firstReq.Response.Note, "还有 1 个问题") {
		t.Fatalf("partial answer %+v", firstReq.Response)
	}
	// A general chat turn would bypass the queue and start a half-answered run.
	if _, e := s.Command(Command{OperationID: "batch-chat", Revision: s.Snapshot().Revision, Type: "send_message", ProjectID: project, TaskID: task, ConversationID: conv, Text: "继续"}); e == nil {
		t.Fatal("general chat bypassed the pending answer batch")
	}
	second, e := s.Command(answerCommand(q2, "batch-answer-2", answers))
	if e != nil {
		t.Fatal(e)
	}
	if second.CreatedID == "" {
		t.Fatal("last answer did not create the aggregated turn")
	}
	st = s.Snapshot()
	c := st.Conversations[conv]
	if len(c.Messages) != 1 || c.Messages[0].ID != second.CreatedID || c.RunState != "starting" {
		t.Fatalf("aggregated turn %+v", c.Messages)
	}
	text := c.Messages[0].Text
	if !strings.Contains(text, "以下是对之前几个问题的答复") || strings.Count(text, "关于「需要确认」的答复") != 2 || strings.Count(text, "端口：3") != 2 {
		t.Fatalf("aggregated text %q", text)
	}
	for _, r := range c.Interactions {
		if r.Response == nil || r.Response.MessageID != second.CreatedID || r.Response.FollowUpRoundID != second.CreatedID {
			t.Fatalf("batch link %+v", r.Response)
		}
	}
	found := false
	for _, r := range c.Rounds {
		if r.ID == second.CreatedID {
			found = true
		}
	}
	if !found {
		t.Fatal("aggregated turn has no round binding")
	}
}

func TestInteractionDeliveryDependencyFollowsAnswerAndCancel(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conv]
		c.Rounds = []Round{{ID: "r1", Phase: "awaiting_user", StartedAt: "2026-10-18T10:00:00Z", FinishedAt: "2026-10-18T10:00:05Z"}}
		st.Conversations[conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	q1 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_dep", RoundID: "r1", DedupeKey: "assistant_question:r1"})
	if _, settled, code, _ := DeliveryDependency(s.Snapshot(), task, "r1"); settled || code != "interaction_pending" {
		t.Fatalf("pending dependency %v %q", settled, code)
	}
	r, e := s.Command(answerCommand(q1, "dep-answer", map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(2)}))
	if e != nil {
		t.Fatal(e)
	}
	// The answer round exists but is still running: delivery must wait.
	if _, settled, code, _ := DeliveryDependency(s.Snapshot(), task, "r1"); settled || code != "interaction_answering" {
		t.Fatalf("answered dependency %v %q", settled, code)
	}
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conv]
		for i := range c.Rounds {
			if c.Rounds[i].ID == r.CreatedID {
				c.Rounds[i].Phase = "finished"
				c.Rounds[i].FinishedAt = "2026-10-18T10:01:00Z"
			}
		}
		st.Conversations[conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, settled, code, _ := DeliveryDependency(s.Snapshot(), task, "r1"); !settled || code != "" {
		t.Fatalf("settled dependency %v %q", settled, code)
	}

	// Cancellation alone never settles; a later explicit finished round does.
	s2, p2, _, t2, c2 := interactionStore(t)
	if e := s2.Update(func(st *State) error {
		c := st.Conversations[c2]
		c.Rounds = []Round{{ID: "c1", Phase: "awaiting_user", StartedAt: "2026-10-18T10:00:00Z", FinishedAt: "2026-10-18T10:00:05Z"}}
		st.Conversations[c2] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	q := seedInteraction(t, s2, p2, t2, c2, InteractionRequest{ID: "ix_dep_cancel", RoundID: "c1", DedupeKey: "assistant_question:c1", AllowCancel: true})
	if _, e := s2.Command(Command{OperationID: "dep-cancel", Revision: 0, Type: "cancel_interaction", ProjectID: p2, TaskID: t2, ConversationID: c2, RequestID: q.ID, RequestVersion: 1}); e != nil {
		t.Fatal(e)
	}
	if _, settled, code, _ := DeliveryDependency(s2.Snapshot(), t2, "c1"); settled || code != "interaction_unanswered" {
		t.Fatalf("cancelled dependency %v %q", settled, code)
	}
	if e := s2.Update(func(st *State) error {
		c := st.Conversations[c2]
		c.Rounds = append(c.Rounds, Round{ID: "c2", Phase: "finished", StartedAt: "2026-10-18T10:02:00Z", FinishedAt: "2026-10-18T10:03:00Z"})
		st.Conversations[c2] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, settled, code, _ := DeliveryDependency(s2.Snapshot(), t2, "c1"); !settled || code != "" {
		t.Fatalf("explicit new turn did not settle %v %q", settled, code)
	}
}

func TestInteractionRestartPreservesConfirmedDeliveryFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p", Path: t.TempDir()}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle", Messages: []Message{}, Rounds: []Round{{ID: "r"}}}
		st.Tasks["t"] = Task{ID: "t", ProjectID: "p", ConversationID: "c"}
		return nil
	})
	for _, req := range []InteractionRequest{
		{ID: "ix_native_ok", Status: InteractionAnswered, Source: SourceAgentUI, Response: &InteractionResponse{Mode: AnswerForm, Delivery: DeliveryAccepted, AnsweredAt: "2026-10-18T10:00:00Z"}, Native: &InteractionNative{Method: "confirm", UpstreamID: "u1", RunID: "run_x1", Generation: 1}},
		{ID: "ix_native_unknown", Status: InteractionAnswered, Source: SourceAgentUI, Response: &InteractionResponse{Mode: AnswerForm, Delivery: DeliveryRecorded, AnsweredAt: "2026-10-18T10:00:00Z"}, Native: &InteractionNative{Method: "confirm", UpstreamID: "u2", RunID: "run_x1", Generation: 1}},
		{ID: "ix_local_cancel", Status: InteractionCancelled, Source: SourceAssistantQuestion, Response: &InteractionResponse{Mode: AnswerCancel, Delivery: DeliveryAccepted, AnsweredAt: "2026-10-18T10:00:00Z"}},
	} {
		r := req
		r.SchemaVersion = 1
		r.Version = 1
		r.ProjectID = "p"
		r.ConversationID = "c"
		r.TaskID = "t"
		r.RoundID = "r"
		r.CreatedAt = "2026-10-18T10:00:00Z"
		r.Blocking = true
		r.AllowCancel = true
		r.Title = "重启"
		r.Fields = []InteractionField{{ID: "allow", Type: FieldConfirm, Label: "是否允许", Required: true}}
		r.DedupeKey = r.ID
		if e := s.Update(func(st *State) error { _, _, err := PublishInteraction(st, r); return err }); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s2, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	byID := map[string]InteractionRequest{}
	for _, r := range s2.Snapshot().Conversations["c"].Interactions {
		byID[r.ID] = r
	}
	if byID["ix_native_ok"].Response.Delivery != DeliveryAccepted {
		t.Fatalf("accepted write was downgraded on restart: %+v", byID["ix_native_ok"].Response)
	}
	if byID["ix_native_unknown"].Response.Delivery != DeliveryUncertain {
		t.Fatalf("unknown window not marked uncertain: %+v", byID["ix_native_unknown"].Response)
	}
	if byID["ix_local_cancel"].Response.Delivery != DeliveryAccepted || byID["ix_local_cancel"].Status != InteractionCancelled {
		t.Fatalf("local cancel changed on restart: %+v", byID["ix_local_cancel"])
	}
}

func TestInteractionNativeAnswerIsNotHeldByOrdinaryQueue(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	ordinary := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_ord", RoundID: "r1", DedupeKey: "assistant_question:r1"})
	native := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_nat", RoundID: "r1", Source: SourceAgentUI, DedupeKey: "agent_ui:run_nat:u1", Fields: []InteractionField{{ID: "allow", Type: FieldConfirm, Label: "是否允许", Required: true}}, Native: &InteractionNative{Method: "confirm", UpstreamID: "u1", RunID: "run_nat", Generation: 1}})
	r, e := s.Command(Command{OperationID: "nat-answer", Revision: 0, Type: "answer_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: native.ID, RequestVersion: 1, Answers: map[string]any{"allow": true}})
	if e != nil {
		t.Fatal(e)
	}
	// The upstream waiting request is answered immediately; it is never held
	// back by an unrelated ordinary question batch.
	if r.CreatedID != "" || r.Delivery != DeliveryRecorded {
		t.Fatalf("native answer receipt %+v", r)
	}
	st := s.Snapshot()
	if len(st.Conversations[conv].Messages) != 0 {
		t.Fatal("native answer created a chat message")
	}
	for _, req := range st.Conversations[conv].Interactions {
		switch req.ID {
		case native.ID:
			if req.Status != InteractionAnswered || req.Response == nil || req.Response.MessageID != "" {
				t.Fatalf("native %+v", req.Response)
			}
		case ordinary.ID:
			if req.Status != InteractionPending {
				t.Fatal("ordinary question changed by the native answer")
			}
		}
	}
	_ = ordinary
}

func TestInteractionCancelWithRecordedAnswerNeverFabricatesAnAnswer(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	q1 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_keep", RoundID: "r1", Title: "保留的问题", DedupeKey: "assistant_question:r1"})
	q2 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_drop", RoundID: "r2", Title: "取消的问题", DedupeKey: "assistant_question:r2", AllowCancel: true})
	if _, e := s.Command(answerCommand(q1, "keep-answer", map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(4)})); e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Conversations[conv].Messages) != 0 {
		t.Fatal("answer dispatched before all questions resolved")
	}
	r, e := s.Command(Command{OperationID: "drop-cancel", Revision: 0, Type: "cancel_interaction", ProjectID: project, TaskID: task, ConversationID: conv, RequestID: q2.ID, RequestVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if r.CreatedID == "" {
		t.Fatal("recorded answer was not flushed after the last question resolved")
	}
	c := s.Snapshot().Conversations[conv]
	if len(c.Messages) != 1 {
		t.Fatalf("messages %+v", c.Messages)
	}
	text := c.Messages[0].Text
	if !strings.Contains(text, "保留的问题") || strings.Contains(text, "取消的问题") || strings.Contains(text, "取消") {
		t.Fatalf("cancellation leaked into the answer text: %q", text)
	}
	for _, req := range c.Interactions {
		switch req.ID {
		case q1.ID:
			if req.Response.MessageID != r.CreatedID {
				t.Fatal("answer not linked to the flushed message")
			}
		case q2.ID:
			if req.Status != InteractionCancelled || req.Response.Mode != AnswerCancel || req.Response.Delivery != DeliveryAccepted || req.Response.MessageID != "" {
				t.Fatalf("cancelled record %+v", req.Response)
			}
		}
	}
}

func TestInteractionDeliveryDependencyFailsClosedOnMalformedRound(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	_ = project
	if e := s.Update(func(st *State) error {
		c := st.Conversations[conv]
		c.Rounds = []Round{{ID: "m1", Phase: "finished", InteractionError: "本轮交互结构无效"}}
		st.Conversations[conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, settled, code, _ := DeliveryDependency(s.Snapshot(), task, "m1"); settled || code != "interaction_invalid" {
		t.Fatalf("malformed round %v %q", settled, code)
	}
}

func TestInteractionBatchRejectsUnsendableAggregate(t *testing.T) {
	s, project, _, task, conv := interactionStore(t)
	q1 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_big1", RoundID: "r1", DedupeKey: "assistant_question:r1"})
	q2 := seedInteraction(t, s, project, task, conv, InteractionRequest{ID: "ix_big2", RoundID: "r2", DedupeKey: "assistant_question:r2"})
	long := strings.Repeat("长", 4000)
	answers := map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(1), "details": long}
	if _, e := s.Command(answerCommand(q1, "big-1", answers)); e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Conversations[conv].Messages) != 0 {
		t.Fatal("first big answer dispatched")
	}
	// The second answer would make the aggregated turn exceed the message
	// limit; it must be refused instead of being stored undeliverable.
	if _, e := s.Command(answerCommand(q2, "big-2", answers)); e == nil {
		t.Fatal("unsendable aggregate accepted")
	}
	c := s.Snapshot().Conversations[conv]
	if c.Interactions[1].Status != InteractionPending || len(c.Messages) != 0 {
		t.Fatalf("refused answer was persisted: %+v", c.Interactions[1])
	}
	// A shorter answer can complete the batch.
	short := map[string]any{"allow": true, "network": "same_lan", "checks": []any{"api"}, "port": float64(1), "details": strings.Repeat("短", 100)}
	if _, e := s.Command(answerCommand(q2, "big-3", short)); e != nil {
		t.Fatal(e)
	}
	c = s.Snapshot().Conversations[conv]
	if len(c.Messages) != 1 || c.Interactions[1].Response.MessageID != c.Messages[0].ID {
		t.Fatal("shortened answer did not flush")
	}
}
