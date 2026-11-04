package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Public slice contract under test:
//   CreateSubagentRun / AppendSubagentEvent / SubagentDetail
// The tests below are the fixed interpretation of the spec's bounds, scope and
// restart rules; Task 4 (real dispatch) may only consume this contract.

func isCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}
func isConflict(err error) bool { return isCode(err, "conflict") }
func isInvalid(err error) bool  { return isCode(err, "invalid_input") }
func isMissing(err error) bool  { return isCode(err, "not_found") }

type subagentFixture struct {
	store        *Store
	apply        func(Command) Receipt
	path         string
	project      string
	conversation string
	task         string
	round        string
	session      string
}

func newSubagentFixture(t *testing.T) *subagentFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path, Profile{Provider: "test", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	apply := func(c Command) Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatalf("command %s: %v", c.Type, e)
		}
		return r
	}
	f := &subagentFixture{store: s, apply: apply, path: path}
	f.project = apply(Command{OperationID: "sub-project", Type: "add_project", Name: "子代理", Path: t.TempDir()}).CreatedID
	topic := apply(Command{OperationID: "sub-topic", Type: "add_topic", ProjectID: f.project, Name: "功能"}).CreatedID
	f.task = apply(Command{OperationID: "sub-task", Type: "add_task", ProjectID: f.project, TopicID: topic, Title: "卡片", Request: "需求"}).CreatedID
	for key, c := range s.Snapshot().Conversations {
		if c.TaskID == f.task {
			f.conversation = key
		}
	}
	if f.conversation == "" {
		t.Fatal("missing task conversation")
	}
	message := apply(Command{OperationID: "sub-round", Type: "send_message", ProjectID: f.project, TaskID: f.task, ConversationID: f.conversation, Text: "开始"})
	f.round = message.CreatedID
	f.session = s.Snapshot().Conversations[f.conversation].SessionID
	return f
}

func (f *subagentFixture) summaryFor(runID string) SubagentSummary {
	return SubagentSummary{
		ID: runID, ProjectID: f.project, ConversationID: f.conversation, TaskID: f.task,
		RoundID: f.round, ParentSessionID: f.session, Title: "搜索现有实现",
		TaskSummary: "检查工作台状态", Status: SubagentStatusStarting,
		Profile: Profile{Provider: "test", Model: "fake", Thinking: "high"},
	}
}

func (f *subagentFixture) create(t *testing.T, runID string) SubagentSummary {
	t.Helper()
	summary, err := f.store.CreateSubagentRun(f.summaryFor(runID), "完整任务：检查工作台状态并报告")
	if err != nil {
		t.Fatalf("create %s: %v", runID, err)
	}
	return summary
}

func (f *subagentFixture) append(t *testing.T, runID string, event SubagentEvent) SubagentSummary {
	t.Helper()
	summary, err := f.store.AppendSubagentEvent(f.conversation, runID, event)
	if err != nil {
		t.Fatalf("append %s/%s: %v", runID, event.ID, err)
	}
	return summary
}

func TestSubagentAcceptedEventReplayStaysIdempotent(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-replay")
	f.append(t, "run-replay", SubagentEvent{ID: "r-run", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	f.append(t, "run-replay", SubagentEvent{ID: "r-done", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	revision, version := f.store.Snapshot().Revision, f.store.Snapshot().Subagents["run-replay"].Version
	// Exact replays of accepted status events must be idempotent, even though
	// the current status already moved past them (running -> running) or is
	// terminal (completed -> completed).
	replayed := f.append(t, "run-replay", SubagentEvent{ID: "r-run", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	if replayed.Version != version {
		t.Fatalf("running replay advanced the summary: %+v", replayed)
	}
	replayed = f.append(t, "run-replay", SubagentEvent{ID: "r-done", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if replayed.Version != version || replayed.Status != SubagentStatusCompleted {
		t.Fatalf("terminal replay advanced the summary: %+v", replayed)
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("status replay advanced revision")
	}
	// Content that is bounded on write must still replay by its original text.
	huge := strings.Repeat("好", 20000)
	toolID := strings.Repeat("t", 300)
	toolName := strings.Repeat("名", 300)
	toolInput := strings.Repeat("输", 20000)
	f.append(t, "run-replay", SubagentEvent{ID: "r-huge", Kind: SubagentEventText, Text: huge})
	f.append(t, "run-replay", SubagentEvent{ID: "r-tool", Kind: SubagentEventToolStart, ToolID: toolID, ToolName: toolName, Input: toolInput, Output: "输出"})
	for i := 0; i < 80; i++ {
		f.append(t, "run-replay", SubagentEvent{ID: fmt.Sprintf("budget-%d", i), Kind: SubagentEventText, Text: strings.Repeat("次", 20000)})
	}
	f.append(t, "run-replay", SubagentEvent{ID: "budget-last", Kind: SubagentEventText, Text: strings.Repeat("尾", 20000), Input: "预算输入"})
	// Prove the stored rows really are truncated, otherwise this case proves nothing.
	storedFor := func(eventID string) SubagentEvent {
		t.Helper()
		var body []byte
		if err := f.store.db.QueryRow("SELECT body FROM subagent_events WHERE run_id=? AND event_id=?", "run-replay", eventID).Scan(&body); err != nil {
			t.Fatal(err)
		}
		var stored SubagentEvent
		if err := json.Unmarshal(body, &stored); err != nil {
			t.Fatal(err)
		}
		return stored
	}
	if got := storedFor("r-huge"); len(got.Text) >= len(huge) {
		t.Fatal("event content was not truncated; the replay case proves nothing")
	}
	if got := storedFor("r-tool"); len([]rune(got.ToolID)) > MaxSubagentToolIDRunes {
		t.Fatalf("tool id was not truncated: %d", len([]rune(got.ToolID)))
	}
	if got := storedFor("budget-last"); len(got.Text) >= len(strings.Repeat("尾", 20000)) {
		t.Fatal("budget-limited content was not truncated; the replay case proves nothing")
	}
	revision, version = f.store.Snapshot().Revision, f.store.Snapshot().Subagents["run-replay"].Version
	for _, event := range []SubagentEvent{
		{ID: "r-huge", Kind: SubagentEventText, Text: huge},
		{ID: "r-tool", Kind: SubagentEventToolStart, ToolID: toolID, ToolName: toolName, Input: toolInput, Output: "输出"},
		{ID: "budget-last", Kind: SubagentEventText, Text: strings.Repeat("尾", 20000), Input: "预算输入"},
	} {
		replay, err := f.store.AppendSubagentEvent(f.conversation, "run-replay", event)
		if err != nil {
			t.Fatalf("truncated event %s rejected on replay: %v", event.ID, err)
		}
		if replay.Version != version {
			t.Fatalf("truncated event %s replay advanced the summary: %+v", event.ID, replay)
		}
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("truncated replay advanced revision")
	}
	// Different content with an accepted id is still a conflict.
	for name, event := range map[string]SubagentEvent{
		"status":         {ID: "r-run", Kind: SubagentEventStatus, Status: SubagentStatusFailed},
		"terminal":       {ID: "r-done", Kind: SubagentEventText, Text: "改了内容"},
		"truncated text": {ID: "r-huge", Kind: SubagentEventText, Text: "改了内容"},
		"truncated tool": {ID: "r-tool", Kind: SubagentEventToolStart, ToolID: "other", ToolName: toolName, Input: toolInput, Output: "输出"},
		"budget event":   {ID: "budget-last", Kind: SubagentEventText, Text: strings.Repeat("尾", 19999), Input: "预算输入"},
	} {
		if _, err := f.store.AppendSubagentEvent(f.conversation, "run-replay", event); !isConflict(err) {
			t.Fatalf("different content with accepted %s id accepted: %v", name, err)
		}
	}
}

func TestSubagentTruncatedEventReplayStaysIdempotent(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-trunc")
	huge := strings.Repeat("好", 20000)
	toolID := strings.Repeat("t", 300)
	toolName := strings.Repeat("名", 300)
	toolInput := strings.Repeat("输", 20000)
	f.append(t, "run-trunc", SubagentEvent{ID: "t-huge", Kind: SubagentEventText, Text: huge})
	f.append(t, "run-trunc", SubagentEvent{ID: "t-tool", Kind: SubagentEventToolStart, ToolID: toolID, ToolName: toolName, Input: toolInput})
	for i := 0; i < 80; i++ {
		f.append(t, "run-trunc", SubagentEvent{ID: fmt.Sprintf("t-budget-%d", i), Kind: SubagentEventText, Text: strings.Repeat("次", 20000)})
	}
	f.append(t, "run-trunc", SubagentEvent{ID: "t-budget-last", Kind: SubagentEventText, Text: strings.Repeat("尾", 20000), Input: "预算输入"})
	revision, version := f.store.Snapshot().Revision, f.store.Snapshot().Subagents["run-trunc"].Version
	for _, event := range []SubagentEvent{
		{ID: "t-huge", Kind: SubagentEventText, Text: huge},
		{ID: "t-tool", Kind: SubagentEventToolStart, ToolID: toolID, ToolName: toolName, Input: toolInput},
		{ID: "t-budget-last", Kind: SubagentEventText, Text: strings.Repeat("尾", 20000), Input: "预算输入"},
	} {
		replay, err := f.store.AppendSubagentEvent(f.conversation, "run-trunc", event)
		if err != nil {
			t.Fatalf("truncated event %s rejected on replay: %v", event.ID, err)
		}
		if replay.Version != version {
			t.Fatalf("truncated event %s replay advanced the summary: %+v", event.ID, replay)
		}
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("truncated replay advanced revision")
	}
}

func TestSubagentCreateIsIdempotentAndRejectsConflicts(t *testing.T) {
	f := newSubagentFixture(t)
	created := f.create(t, "run-one")
	if created.Version != 1 || created.Status != SubagentStatusStarting || created.StartedAt == "" || created.UpdatedAt != created.StartedAt || created.Truncated || created.FinishedAt != "" {
		t.Fatalf("unexpected created summary: %+v", created)
	}
	if created.ProjectID != f.project || created.ConversationID != f.conversation || created.TaskID != f.task || created.RoundID != f.round || created.ParentSessionID != f.session {
		t.Fatalf("parent identity lost: %+v", created)
	}
	if created.Profile.Provider != "test" || created.Profile.Model != "fake" || created.Profile.Thinking != "high" || created.Profile.CredentialSource != "" {
		t.Fatalf("profile snapshot wrong or credential leaked: %+v", created.Profile)
	}
	revision := f.store.Snapshot().Revision
	again, err := f.store.CreateSubagentRun(f.summaryFor("run-one"), "完整任务：检查工作台状态并报告")
	if err != nil || again.Version != 1 || again.StartedAt != created.StartedAt {
		t.Fatalf("idempotent replay changed the run: %+v %v", again, err)
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("idempotent replay advanced revision")
	}
	conflict := f.summaryFor("run-one")
	conflict.TaskSummary = "另一个任务"
	if _, err = f.store.CreateSubagentRun(conflict, "完整任务：检查工作台状态并报告"); !isConflict(err) {
		t.Fatalf("same id different summary accepted: %v", err)
	}
	if _, err = f.store.CreateSubagentRun(f.summaryFor("run-one"), "不同的任务正文"); !isConflict(err) {
		t.Fatalf("same id different request accepted: %v", err)
	}
	rephrased := f.summaryFor("run-one")
	rephrased.Title = " 搜索现有实现 "
	if replay, err := f.store.CreateSubagentRun(rephrased, "完整任务：检查工作台状态并报告"); err != nil || replay.StartedAt != created.StartedAt {
		t.Fatalf("whitespace-normalized replay must stay idempotent: %+v %v", replay, err)
	}
}

func TestSubagentCreateValidatesOwnershipAndEnums(t *testing.T) {
	f := newSubagentFixture(t)
	other := f.apply(Command{OperationID: "sub-other-project", Type: "add_project", Name: "另一个", Path: t.TempDir()}).CreatedID
	otherConversation := f.store.Snapshot().Projects[other].ConversationID
	for name, mutate := range map[string]func(*SubagentSummary){
		"foreign project":      func(s *SubagentSummary) { s.ProjectID = other },
		"foreign conversation": func(s *SubagentSummary) { s.ConversationID = otherConversation },
		"cross project":        func(s *SubagentSummary) { s.ProjectID = other },
		"unknown task":         func(s *SubagentSummary) { s.TaskID = "nope" },
		"unknown round":        func(s *SubagentSummary) { s.RoundID = "nope" },
		"foreign session":      func(s *SubagentSummary) { s.ParentSessionID = "kaiwu_other" },
		"empty session":        func(s *SubagentSummary) { s.ParentSessionID = "" },
	} {
		input := f.summaryFor("run-scope")
		mutate(&input)
		if _, err := f.store.CreateSubagentRun(input, "正文"); !isMissing(err) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	crossed := f.summaryFor("run-cross")
	crossed.ProjectID = other
	crossed.TaskID = f.task
	if _, err := f.store.CreateSubagentRun(crossed, "正文"); !isMissing(err) {
		t.Fatalf("cross-project task binding accepted: %v", err)
	}
	for name, mutate := range map[string]func(*SubagentSummary){
		"empty id":          func(s *SubagentSummary) { s.ID = "" },
		"slash id":          func(s *SubagentSummary) { s.ID = "run/one" },
		"blank title":       func(s *SubagentSummary) { s.Title = "  " },
		"long title":        func(s *SubagentSummary) { s.Title = strings.Repeat("标", MaxSubagentTitleRunes+1) },
		"long task summary": func(s *SubagentSummary) { s.TaskSummary = strings.Repeat("好", MaxSubagentSummaryRunes+1) },
		"terminal status":   func(s *SubagentSummary) { s.Status = SubagentStatusCompleted },
		"unknown status":    func(s *SubagentSummary) { s.Status = "queued" },
		"empty provider":    func(s *SubagentSummary) { s.Profile.Provider = "" },
		"empty model":       func(s *SubagentSummary) { s.Profile.Model = "" },
		"bad thinking":      func(s *SubagentSummary) { s.Profile.Thinking = "extreme" },
	} {
		input := f.summaryFor("run-bad")
		mutate(&input)
		if _, err := f.store.CreateSubagentRun(input, "正文"); !isInvalid(err) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if _, err := f.store.CreateSubagentRun(f.summaryFor("run-oversize"), strings.Repeat("需", MaxSubagentRequestBytes)); !isInvalid(err) {
		t.Fatalf("oversized request accepted: %v", err)
	}
	if _, err := f.store.CreateSubagentRun(f.summaryFor("run-invalid-utf8"), string([]byte{0xff, 0xfe})); !isInvalid(err) {
		t.Fatalf("invalid request encoding accepted: %v", err)
	}
	// TaskID is derived from the conversation when the caller omits it, and the
	// derivation is part of the idempotency fingerprint.
	inherited := f.summaryFor("run-inherit")
	inherited.TaskID = ""
	stored, err := f.store.CreateSubagentRun(inherited, "正文")
	if err != nil || stored.TaskID != f.task {
		t.Fatalf("task binding not derived: %+v %v", stored, err)
	}
	explicit := f.summaryFor("run-inherit")
	if replay, err := f.store.CreateSubagentRun(explicit, "正文"); err != nil || replay.StartedAt != stored.StartedAt {
		t.Fatalf("explicit task replay conflicted with derived task: %+v %v", replay, err)
	}
	// A project conversation round also has a legal parent identity.
	otherRound := f.apply(Command{OperationID: "sub-project-round", Type: "send_message", ProjectID: f.project, ConversationID: f.store.Snapshot().Projects[f.project].ConversationID, Text: "项目对话"})
	projectRun := f.summaryFor("run-project-talk")
	projectRun.ConversationID = f.store.Snapshot().Projects[f.project].ConversationID
	projectRun.TaskID = ""
	projectRun.RoundID = otherRound.CreatedID
	projectRun.ParentSessionID = f.store.Snapshot().Conversations[projectRun.ConversationID].SessionID
	if projectSummary, err := f.store.CreateSubagentRun(projectRun, "正文"); err != nil || projectSummary.TaskID != "" {
		t.Fatalf("project conversation run rejected: %+v %v", projectSummary, err)
	}
}

func TestSubagentEventIdempotencyTransitionsAndTerminalStability(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-events")
	first := f.append(t, "run-events", SubagentEvent{ID: "e-1", Kind: SubagentEventText, Text: "第一段"})
	if first.Version != 2 || first.UpdatedAt == "" {
		t.Fatalf("append did not advance the summary: %+v", first)
	}
	revision := f.store.Snapshot().Revision
	replay := f.append(t, "run-events", SubagentEvent{ID: "e-1", Kind: SubagentEventText, Text: "第一段"})
	if replay.Version != first.Version || replay.UpdatedAt != first.UpdatedAt {
		t.Fatalf("event replay changed the summary: %+v", replay)
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("event replay advanced revision")
	}
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-events", SubagentEvent{ID: "e-1", Kind: SubagentEventText, Text: "同一标识不同内容"}); !isConflict(err) {
		t.Fatalf("same event id with other text accepted: %v", err)
	}
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-events", SubagentEvent{ID: "e-1", Kind: SubagentEventStatus, Status: SubagentStatusRunning}); !isConflict(err) {
		t.Fatalf("same event id with other kind accepted: %v", err)
	}
	f.append(t, "run-events", SubagentEvent{ID: "e-2", Kind: SubagentEventToolStart, ToolID: "tool-1", ToolName: "read", Input: `{"path":"a.go"}`})
	f.append(t, "run-events", SubagentEvent{ID: "e-3", Kind: SubagentEventToolEnd, ToolID: "tool-1", ToolName: "read", Output: "ok"})
	if running := f.append(t, "run-events", SubagentEvent{ID: "e-4", Kind: SubagentEventStatus, Status: SubagentStatusRunning}); running.Status != SubagentStatusRunning {
		t.Fatalf("starting -> running rejected: %+v", running)
	}
	completed := f.append(t, "run-events", SubagentEvent{ID: "e-5", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if completed.Status != SubagentStatusCompleted || completed.FinishedAt == "" {
		t.Fatalf("running -> completed not recorded: %+v", completed)
	}
	revision = f.store.Snapshot().Revision
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-events", SubagentEvent{ID: "e-6", Kind: SubagentEventStatus, Status: SubagentStatusRunning}); !isConflict(err) {
		t.Fatalf("terminal run revived: %v", err)
	}
	if f.store.Snapshot().Revision != revision || f.store.Snapshot().Subagents["run-events"].Status != SubagentStatusCompleted {
		t.Fatal("refused revival mutated the summary")
	}
	late := f.append(t, "run-events", SubagentEvent{ID: "e-7", Kind: SubagentEventText, Text: "结束后的迟到输出"})
	if late.Status != SubagentStatusCompleted {
		t.Fatalf("late evidence changed terminal status: %+v", late)
	}
	page, err := f.store.SubagentDetail(f.project, f.conversation, "run-events", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// e-6 was refused as a terminal revival and is never stored.
	if len(page.Events) != 6 || page.Events[5].ID != "e-7" {
		t.Fatalf("late evidence not persisted: %+v", page.Events)
	}
	for i, event := range page.Events {
		if event.Sequence != uint64(i+1) || event.At == "" {
			t.Fatalf("event ordering wrong at %d: %+v", i, event)
		}
	}
	if page.Events[1].ToolName != "read" || page.Events[1].Input == "" || page.Events[2].Output != "ok" {
		t.Fatalf("tool fields lost: %+v", page.Events)
	}
	f.create(t, "run-skip")
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-skip", SubagentEvent{ID: "s-1", Kind: SubagentEventStatus, Status: SubagentStatusCompleted}); !isConflict(err) {
		t.Fatalf("starting -> completed accepted: %v", err)
	}
	f.create(t, "run-unknown")
	f.append(t, "run-unknown", SubagentEvent{ID: "u-1", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	f.append(t, "run-unknown", SubagentEvent{ID: "u-2", Kind: SubagentEventStatus, Status: SubagentStatusUnknown})
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-unknown", SubagentEvent{ID: "u-3", Kind: SubagentEventStatus, Status: SubagentStatusRunning}); !isConflict(err) {
		t.Fatalf("unknown auto-recovered: %v", err)
	}
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-unknown", SubagentEvent{ID: "u-4", Kind: SubagentEventStatus, Status: SubagentStatusFailed}); !isConflict(err) {
		t.Fatalf("unknown accepted a second terminal state: %v", err)
	}
	f.create(t, "run-interrupted")
	f.append(t, "run-interrupted", SubagentEvent{ID: "i-1", Kind: SubagentEventStatus, Status: SubagentStatusInterrupted})
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-interrupted", SubagentEvent{ID: "i-2", Kind: SubagentEventStatus, Status: SubagentStatusRunning}); !isConflict(err) {
		t.Fatalf("interrupted auto-recovered: %v", err)
	}
	f.create(t, "run-other")
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-other", SubagentEvent{ID: "e-1", Kind: SubagentEventText, Text: "另一个运行的同一事件标识"}); err != nil {
		t.Fatalf("event ids must be scoped per run: %v", err)
	}
	for name, event := range map[string]SubagentEvent{
		"unknown kind":          {ID: "b-1", Kind: "thinking", Text: "private"},
		"status without status": {ID: "b-2", Kind: SubagentEventStatus},
		"status on text":        {ID: "b-3", Kind: SubagentEventText, Text: "x", Status: SubagentStatusRunning},
		"bad status":            {ID: "b-4", Kind: SubagentEventStatus, Status: "queued"},
		"empty id":              {ID: "", Kind: SubagentEventText, Text: "x"},
		"slash id":              {ID: "e/1", Kind: SubagentEventText, Text: "x"},
		"invalid text":          {ID: "b-5", Kind: SubagentEventText, Text: string([]byte{0xff})},
	} {
		if _, err := f.store.AppendSubagentEvent(f.conversation, "run-other", event); !isInvalid(err) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if _, err := f.store.AppendSubagentEvent(f.conversation, "missing-run", SubagentEvent{ID: "x-1", Kind: SubagentEventText, Text: "x"}); !isMissing(err) {
		t.Fatalf("unknown run accepted an event: %v", err)
	}
	if _, err := f.store.AppendSubagentEvent("other-conversation", "run-other", SubagentEvent{ID: "x-2", Kind: SubagentEventText, Text: "x"}); !isMissing(err) {
		t.Fatalf("foreign conversation accepted an event: %v", err)
	}
}

func TestSubagentDetailScopedPagingAndLimits(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-page")
	for i := 1; i <= 120; i++ {
		f.append(t, "run-page", SubagentEvent{ID: fmt.Sprintf("p-%d", i), Kind: SubagentEventText, Text: fmt.Sprintf("事件 %d", i)})
	}
	snapshot := f.store.Snapshot()
	revision, version := snapshot.Revision, snapshot.Subagents["run-page"].Version
	first, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 50 || first.Next != 50 || !first.HasMore {
		t.Fatalf("default page wrong: %+v", first)
	}
	if first.Request != "完整任务：检查工作台状态并报告" || first.Run.ID != "run-page" || first.Run.Version != version {
		t.Fatalf("detail identity wrong: %+v", first)
	}
	if first.Events[0].Sequence != 1 || first.Events[0].At == "" || first.Events[0].Kind != SubagentEventText {
		t.Fatalf("first event wrong: %+v", first.Events[0])
	}
	second, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", first.Next, 50)
	if err != nil || len(second.Events) != 50 || second.Events[0].Sequence != 51 || !second.HasMore {
		t.Fatalf("second page wrong: %+v %v", second, err)
	}
	third, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", second.Next, 50)
	if err != nil || len(third.Events) != 20 || third.Events[0].Sequence != 101 || third.HasMore || third.Next != 120 {
		t.Fatalf("third page wrong: %+v %v", third, err)
	}
	empty, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", third.Next, 50)
	if err != nil || len(empty.Events) != 0 || empty.HasMore || empty.Next != 120 {
		t.Fatalf("tail page wrong: %+v %v", empty, err)
	}
	seen := map[uint64]bool{}
	after := uint64(0)
	for {
		page, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", after, 7)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			if seen[event.Sequence] || event.Sequence <= after {
				t.Fatalf("page replayed sequence %d after %d", event.Sequence, after)
			}
			seen[event.Sequence] = true
		}
		if !page.HasMore {
			break
		}
		after = page.Next
	}
	if len(seen) != 120 {
		t.Fatalf("paging lost events: %d", len(seen))
	}
	if _, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", 0, -1); !isInvalid(err) {
		t.Fatalf("negative limit accepted: %v", err)
	}
	if _, err := f.store.SubagentDetail(f.project, f.conversation, "run-page", 0, MaxSubagentDetailLimit+1); !isInvalid(err) {
		t.Fatalf("oversized limit accepted: %v", err)
	}
	if f.store.Snapshot().Revision != revision || f.store.Snapshot().Subagents["run-page"].Version != version {
		t.Fatal("read path mutated state")
	}
	if _, err := f.store.SubagentDetail("other", f.conversation, "run-page", 0, 0); !isMissing(err) {
		t.Fatalf("foreign project read accepted: %v", err)
	}
	if _, err := f.store.SubagentDetail(f.project, "other", "run-page", 0, 0); !isMissing(err) {
		t.Fatalf("foreign conversation read accepted: %v", err)
	}
	if _, err := f.store.SubagentDetail(f.project, f.conversation, "missing", 0, 0); !isMissing(err) {
		t.Fatalf("unknown run read accepted: %v", err)
	}
	otherProject := f.apply(Command{OperationID: "sub-detail-other", Type: "add_project", Name: "隔离", Path: t.TempDir()}).CreatedID
	otherConversation := f.store.Snapshot().Projects[otherProject].ConversationID
	otherRound := f.apply(Command{OperationID: "sub-detail-round", Type: "send_message", ProjectID: otherProject, ConversationID: otherConversation, Text: "另一个项目"})
	foreign := SubagentSummary{ID: "run-foreign", ProjectID: otherProject, ConversationID: otherConversation, RoundID: otherRound.CreatedID, ParentSessionID: f.store.Snapshot().Conversations[otherConversation].SessionID, Title: "别的", TaskSummary: "别的任务", Status: SubagentStatusStarting, Profile: Profile{Provider: "test", Model: "fake"}}
	if _, err := f.store.CreateSubagentRun(foreign, "别的正文"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SubagentDetail(f.project, f.conversation, "run-foreign", 0, 0); !isMissing(err) {
		t.Fatalf("cross-project run readable: %v", err)
	}
	if page, err := f.store.SubagentDetail(otherProject, otherConversation, "run-foreign", 0, 0); err != nil || page.Request != "别的正文" || page.Run.TaskID != "" {
		t.Fatalf("legitimate foreign read failed: %+v %v", page, err)
	}
}

func TestSubagentEventBoundsTruncateAndKeepTerminalState(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-bounds")
	summary := f.append(t, "run-bounds", SubagentEvent{ID: "e-huge", Kind: SubagentEventText, Text: strings.Repeat("好", 20000)})
	if !summary.Truncated {
		t.Fatal("oversized event did not mark truncation")
	}
	page, err := f.store.SubagentDetail(f.project, f.conversation, "run-bounds", 0, 0)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("oversized event not stored: %+v %v", page, err)
	}
	if len(page.Events[0].Text) > MaxSubagentEventContentBytes || !utf8.ValidString(page.Events[0].Text) {
		t.Fatalf("event content not bounded: %d", len(page.Events[0].Text))
	}
	for i := 0; i < 200; i++ {
		f.append(t, "run-bounds", SubagentEvent{ID: fmt.Sprintf("bulk-%d", i), Kind: SubagentEventText, Text: strings.Repeat("次", 20000), Input: `{"n":1}`, Output: "输出"})
	}
	var cumulative int
	rows, err := f.store.db.Query("SELECT body FROM subagent_events WHERE run_id=?", "run-bounds")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var body []byte
		var event SubagentEvent
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		cumulative += len(event.Text) + len(event.Input) + len(event.Output)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if cumulative > MaxSubagentRunContentBytes {
		t.Fatalf("cumulative content exceeded the run budget: %d", cumulative)
	}
	if !f.store.Snapshot().Subagents["run-bounds"].Truncated {
		t.Fatal("cumulative truncation not flagged")
	}
	running := f.append(t, "run-bounds", SubagentEvent{ID: "e-running", Kind: SubagentEventStatus, Status: SubagentStatusRunning, Text: strings.Repeat("结", 20000)})
	if running.Status != SubagentStatusRunning {
		t.Fatalf("status transition blocked by content budget: %+v", running)
	}
	completed := f.append(t, "run-bounds", SubagentEvent{ID: "e-completed", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if completed.Status != SubagentStatusCompleted || completed.FinishedAt == "" {
		t.Fatalf("terminal state blocked by content budget: %+v", completed)
	}
	var terminal int
	if err := f.store.db.QueryRow("SELECT count(*) FROM subagent_events WHERE run_id=? AND event_id='e-completed'", "run-bounds").Scan(&terminal); err != nil {
		t.Fatal(err)
	}
	if terminal != 1 {
		t.Fatal("terminal status event was not persisted")
	}
}

func fillSubagentEventsToCap(t *testing.T, f *subagentFixture, runID string) {
	t.Helper()
	tx, err := f.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < MaxSubagentEvents; i++ {
		if _, err := tx.Exec("INSERT INTO subagent_events(run_id,sequence,event_id,fingerprint,body) VALUES(?,?,?,?,?)", runID, i+1, fmt.Sprintf("cap-%d", i), "seed", []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func subagentEventRows(t *testing.T, f *subagentFixture, runID, eventID string) int {
	t.Helper()
	var n int
	if err := f.store.db.QueryRow("SELECT count(*) FROM subagent_events WHERE run_id=? AND event_id=?", runID, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubagentOverflowStatusReplayIsIdempotentAndBounded(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-overflow")
	fillSubagentEventsToCap(t, f, "run-overflow")
	// A non-status event at the cap is dropped and deliberately not recorded, so
	// an exact replay stays a drop and no conflict can be claimed for it.
	dropped := f.append(t, "run-overflow", SubagentEvent{ID: "drop-1", Kind: SubagentEventText, Text: "第一次"})
	if !dropped.Truncated {
		t.Fatalf("dropped event was not flagged: %+v", dropped)
	}
	revision := f.store.Snapshot().Revision
	if replay := f.append(t, "run-overflow", SubagentEvent{ID: "drop-1", Kind: SubagentEventText, Text: "第二次不同内容"}); !replay.Truncated {
		t.Fatalf("dropped replay lost the truncation flag: %+v", replay)
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("dropped event replay rewrote state")
	}
	if n := subagentEventRows(t, f, "run-overflow", "drop-1"); n != 0 {
		t.Fatal("dropped event was recorded")
	}
	// The first accepted overflow status keeps a bounded fingerprint.
	running := f.append(t, "run-overflow", SubagentEvent{ID: "of-1", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	if running.Status != SubagentStatusRunning {
		t.Fatalf("overflow status refused: %+v", running)
	}
	revision, version := f.store.Snapshot().Revision, running.Version
	replay := f.append(t, "run-overflow", SubagentEvent{ID: "of-1", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	if replay.Version != version || f.store.Snapshot().Revision != revision {
		t.Fatal("overflow status replay advanced state")
	}
	// Without the retained fingerprint, starting -> failed would look legal.
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-overflow", SubagentEvent{ID: "of-1", Kind: SubagentEventStatus, Status: SubagentStatusFailed}); !isConflict(err) {
		t.Fatalf("different status with an accepted overflow id accepted: %v", err)
	}
	// A terminal transition at the cap is still saved, and its replay too.
	completed := f.append(t, "run-overflow", SubagentEvent{ID: "of-2", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if completed.Status != SubagentStatusCompleted || completed.FinishedAt == "" {
		t.Fatalf("overflow terminal state lost: %+v", completed)
	}
	revision, version = f.store.Snapshot().Revision, completed.Version
	replay = f.append(t, "run-overflow", SubagentEvent{ID: "of-2", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if replay.Version != version || f.store.Snapshot().Revision != revision {
		t.Fatal("overflow terminal replay advanced state")
	}
	if n := subagentEventRows(t, f, "run-overflow", "of-1") + subagentEventRows(t, f, "run-overflow", "of-2"); n != 0 {
		t.Fatal("overflow status events were stored as rows")
	}
	var rowBody []byte
	if err := f.store.db.QueryRow("SELECT body FROM subagent_runs WHERE id=?", "run-overflow").Scan(&rowBody); err != nil {
		t.Fatal(err)
	}
	var record subagentRecord
	if err := json.Unmarshal(rowBody, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Overflow) != 2 || len(record.Overflow) > maxSubagentOverflowFingerprints {
		t.Fatalf("overflow fingerprints are not bounded: %+v", record.Overflow)
	}
	if len(rowBody) > 2048 {
		t.Fatalf("overflow fingerprints grew the run row: %d bytes", len(rowBody))
	}
}

func TestSubagentEventCountCapKeepsTerminalState(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-cap")
	// Fill the stored event table to the hard cap in one transaction. The
	// contract under test is the admission decision at the cap, not append
	// throughput; the two status transitions below are the real appends.
	fillSubagentEventsToCap(t, f, "run-cap")
	count := func() int {
		t.Helper()
		var n int
		if err := f.store.db.QueryRow("SELECT count(*) FROM subagent_events WHERE run_id=?", "run-cap").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != MaxSubagentEvents {
		t.Fatalf("seed failed: %d", count())
	}
	dropped := f.append(t, "run-cap", SubagentEvent{ID: "cap-extra", Kind: SubagentEventText, Text: "超限内容"})
	if !dropped.Truncated {
		t.Fatal("dropped content was not flagged as truncated")
	}
	if count() != MaxSubagentEvents {
		t.Fatalf("event cap not enforced: %d", count())
	}
	var extras int
	if err := f.store.db.QueryRow("SELECT count(*) FROM subagent_events WHERE run_id=? AND event_id='cap-extra'", "run-cap").Scan(&extras); err != nil {
		t.Fatal(err)
	}
	if extras != 0 {
		t.Fatal("over-cap event was stored")
	}
	revision := f.store.Snapshot().Revision
	if again := f.append(t, "run-cap", SubagentEvent{ID: "cap-extra-2", Kind: SubagentEventText, Text: "再一个"}); !again.Truncated {
		t.Fatalf("second dropped event lost the truncation flag: %+v", again)
	}
	if f.store.Snapshot().Revision != revision {
		t.Fatal("repeated dropped events keep rewriting state")
	}
	running := f.append(t, "run-cap", SubagentEvent{ID: "cap-running", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	if running.Status != SubagentStatusRunning {
		t.Fatalf("status transition blocked by the event cap: %+v", running)
	}
	completed := f.append(t, "run-cap", SubagentEvent{ID: "cap-completed", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	if completed.Status != SubagentStatusCompleted || completed.FinishedAt == "" {
		t.Fatalf("terminal state blocked by the event cap: %+v", completed)
	}
	if count() != MaxSubagentEvents {
		t.Fatalf("status events grew the stored event count: %d", count())
	}
	var cumulative int
	rows, err := f.store.db.Query("SELECT body FROM subagent_events WHERE run_id=?", "run-cap")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var body []byte
		var event SubagentEvent
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		cumulative += len(event.Text) + len(event.Input) + len(event.Output)
	}
	_ = rows.Close()
	if cumulative > MaxSubagentRunContentBytes {
		t.Fatalf("empty events bypassed the content budget: %d", cumulative)
	}
}

func TestSubagentRestartMarksActiveUnknownAndKeepsTerminal(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-active")
	f.append(t, "run-active", SubagentEvent{ID: "a-1", Kind: SubagentEventText, Text: "进度"})
	f.append(t, "run-active", SubagentEvent{ID: "a-2", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	f.create(t, "run-done")
	f.append(t, "run-done", SubagentEvent{ID: "d-1", Kind: SubagentEventStatus, Status: SubagentStatusRunning})
	done := f.append(t, "run-done", SubagentEvent{ID: "d-2", Kind: SubagentEventStatus, Status: SubagentStatusCompleted})
	f.create(t, "run-interrupted")
	f.append(t, "run-interrupted", SubagentEvent{ID: "i-1", Kind: SubagentEventStatus, Status: SubagentStatusInterrupted})
	before := f.store.Snapshot()
	activeVersion := before.Subagents["run-active"].Version
	events, err := f.store.SubagentDetail(f.project, f.conversation, "run-active", 0, 0)
	if err != nil || len(events.Events) != 2 {
		t.Fatalf("pre-restart detail wrong: %+v %v", events, err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snapshot := reopened.Snapshot()
	active := snapshot.Subagents["run-active"]
	if active.Status != SubagentStatusUnknown || active.FinishedAt == "" || active.Version != activeVersion+1 {
		t.Fatalf("active run not normalized to unknown: %+v", active)
	}
	if active.ConversationID != f.conversation || active.ProjectID != f.project || active.TaskID != f.task || active.RoundID != f.round || active.ParentSessionID != f.session {
		t.Fatalf("restart lost parent identity: %+v", active)
	}
	if done.Status != SubagentStatusCompleted || snapshot.Subagents["run-done"] != done {
		t.Fatalf("terminal state changed across restart: %+v vs %+v", snapshot.Subagents["run-done"], done)
	}
	if snapshot.Subagents["run-interrupted"].Status != SubagentStatusInterrupted {
		t.Fatalf("interrupted state changed across restart: %+v", snapshot.Subagents["run-interrupted"])
	}
	if snapshot.Tasks[f.task].Status == "" || snapshot.Conversations[f.conversation].Rounds[0].Phase != "interrupted" {
		t.Fatal("restart normalization skipped existing state")
	}
	page, err := reopened.SubagentDetail(f.project, f.conversation, "run-active", 0, 0)
	if err != nil || len(page.Events) != 2 || page.Request != "完整任务：检查工作台状态并报告" {
		t.Fatalf("restart lost detail history: %+v %v", page, err)
	}
	if _, err := reopened.AppendSubagentEvent(f.conversation, "run-active", SubagentEvent{ID: "a-3", Kind: SubagentEventStatus, Status: SubagentStatusCompleted}); !isConflict(err) {
		t.Fatalf("unknown run recovered after restart: %v", err)
	}
}

func TestSubagentSnapshotAndPatchesCarrySummaryOnly(t *testing.T) {
	f := newSubagentFixture(t)
	const request = "TOP-SECRET-REQUEST-正文"
	const eventText = "SECRET-EVENT-TEXT-可见回复"
	before := f.store.Snapshot().Revision
	if _, err := f.store.CreateSubagentRun(f.summaryFor("run-summary"), request); err != nil {
		t.Fatal(err)
	}
	f.append(t, "run-summary", SubagentEvent{ID: "s-1", Kind: SubagentEventText, Text: eventText})
	snapshot := f.store.Snapshot()
	if snapshot.Subagents["run-summary"].Title != "搜索现有实现" {
		t.Fatalf("summary missing from state: %+v", snapshot.Subagents)
	}
	stateJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateJSON), request) || strings.Contains(string(stateJSON), eventText) {
		t.Fatal("snapshot leaked request or event content")
	}
	if !strings.Contains(string(stateJSON), `"run-summary"`) || !strings.Contains(string(stateJSON), `"subagents"`) {
		t.Fatal("snapshot lost the summary projection")
	}
	var rawBody []byte
	if err := f.store.db.QueryRow("SELECT body FROM state WHERE id=1").Scan(&rawBody); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawBody), request) || strings.Contains(string(rawBody), eventText) {
		t.Fatal("durable state row leaked request or event content")
	}
	deltas, reset := f.store.SyncSince(before)
	patched, _ := json.Marshal(deltas)
	if strings.Contains(string(patched), request) || strings.Contains(string(patched), eventText) {
		t.Fatal("patch stream leaked request or event content")
	}
	if reset != nil {
		t.Fatal("unexpected reset for a live patch window")
	}
	found := false
	for _, delta := range deltas {
		for _, op := range delta.Ops {
			if len(op.Path) == 2 && op.Path[0] == "subagents" && op.Path[1] == "run-summary" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("patch did not add the lightweight summary: %+v", deltas)
	}
	allRaw, resetAll := f.store.SyncSince(0)
	all, _ := json.Marshal(allRaw)
	if resetAll != nil {
		all, _ = json.Marshal(resetAll)
	}
	if strings.Contains(string(all), request) || strings.Contains(string(all), eventText) {
		t.Fatal("replay window leaked request or event content")
	}
	if strings.Contains(string(all), "完整任务") {
		t.Fatal("replay window leaked the run request")
	}
}

func TestSubagentsAreNotCreatedByChatOrCommands(t *testing.T) {
	f := newSubagentFixture(t)
	for _, typ := range []string{"create_subagent", "append_subagent_event", "subagents/detail", "subagent_run"} {
		_, err := f.store.Command(Command{OperationID: "cmd-" + strings.NewReplacer("/", "-", "_", "-").Replace(typ), Type: typ, ProjectID: f.project, ConversationID: f.conversation, Revision: f.store.Snapshot().Revision})
		if !isInvalid(err) {
			t.Fatalf("command %s accepted: %v", typ, err)
		}
	}
	projectConversation := f.store.Snapshot().Projects[f.project].ConversationID
	if _, err := f.store.Command(Command{OperationID: "chat-subagent", Type: "send_message", ProjectID: f.project, ConversationID: projectConversation, Text: "◉ 3 个子 Agent 运行中：run-1 搜索实现，run-2 检查测试", Revision: f.store.Snapshot().Revision}); err != nil {
		t.Fatal(err)
	}
	if len(f.store.Snapshot().Subagents) != 0 {
		t.Fatal("chat text created subagent records")
	}
}

func TestSubagentAdmissionLimitsKeepHistory(t *testing.T) {
	f := newSubagentFixture(t)
	created := f.create(t, "run-limit")
	seed := func(conversation string, count int, prefix string) {
		t.Helper()
		tx, err := f.store.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < count; i++ {
			if _, err := tx.Exec("INSERT INTO subagent_runs(id,project_id,conversation_id,task_id,round_id,parent_session_id,body) VALUES(?,?,?,?,?,?,?)", fmt.Sprintf("%s-%d", prefix, i), f.project, conversation, f.task, f.round, f.session, []byte("{}")); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	countRows := func(conversation string) int {
		t.Helper()
		var n int
		if err := f.store.db.QueryRow("SELECT count(*) FROM subagent_runs WHERE conversation_id=?", conversation).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	seed(f.conversation, MaxSubagentRunsPerConversation-1, "seed")
	if countRows(f.conversation) != MaxSubagentRunsPerConversation {
		t.Fatalf("seed failed: %d", countRows(f.conversation))
	}
	if _, err := f.store.CreateSubagentRun(f.summaryFor("run-over-conversation"), "正文"); !isInvalid(err) {
		t.Fatalf("per-conversation cap accepted a new run: %v", err)
	}
	if countRows(f.conversation) != MaxSubagentRunsPerConversation {
		t.Fatal("admission limit deleted stored history")
	}
	if replay, err := f.store.CreateSubagentRun(f.summaryFor("run-limit"), "完整任务：检查工作台状态并报告"); err != nil || replay.StartedAt != created.StartedAt {
		t.Fatalf("idempotent replay rejected at the cap: %+v %v", replay, err)
	}
	seed("bulk-conversation", MaxSubagentRuns-1, "bulk")
	projectConversation := f.store.Snapshot().Projects[f.project].ConversationID
	round := f.apply(Command{OperationID: "sub-limit-round", Type: "send_message", ProjectID: f.project, ConversationID: projectConversation, Text: "项目对话"})
	global := f.summaryFor("run-over-global")
	global.ConversationID = projectConversation
	global.TaskID = ""
	global.RoundID = round.CreatedID
	global.ParentSessionID = f.store.Snapshot().Conversations[projectConversation].SessionID
	if _, err := f.store.CreateSubagentRun(global, "正文"); !isInvalid(err) {
		t.Fatalf("global cap accepted a new run: %v", err)
	}
	if countRows("bulk-conversation") != MaxSubagentRuns-1 {
		t.Fatal("global admission limit deleted stored history")
	}
}

func TestSubagentAppendRollsBackWithDetailWrite(t *testing.T) {
	f := newSubagentFixture(t)
	f.create(t, "run-atomic")
	if _, err := f.store.db.Exec("CREATE TRIGGER subagent_events_boom BEFORE INSERT ON subagent_events BEGIN SELECT RAISE(ABORT, 'boom'); END"); err != nil {
		t.Fatal(err)
	}
	revision := f.store.Snapshot().Revision
	version := f.store.Snapshot().Subagents["run-atomic"].Version
	if _, err := f.store.AppendSubagentEvent(f.conversation, "run-atomic", SubagentEvent{ID: "atomic-1", Kind: SubagentEventText, Text: "写入应回滚"}); err == nil {
		t.Fatal("failed detail write reported success")
	}
	if f.store.Snapshot().Revision != revision || f.store.Snapshot().Subagents["run-atomic"].Version != version {
		t.Fatal("summary advanced although the detail write failed")
	}
	page, err := f.store.SubagentDetail(f.project, f.conversation, "run-atomic", 0, 0)
	if err != nil || len(page.Events) != 0 {
		t.Fatalf("detail row leaked from a rolled back transaction: %+v %v", page, err)
	}
	if _, err := f.store.db.Exec("DROP TRIGGER subagent_events_boom"); err != nil {
		t.Fatal(err)
	}
	summary := f.append(t, "run-atomic", SubagentEvent{ID: "atomic-1", Kind: SubagentEventText, Text: "现在可以写入"})
	if summary.Version != version+1 {
		t.Fatalf("append did not recover after the failure: %+v", summary)
	}
}

const schema2DDL = `
 CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS operations (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, receipt BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS login_sessions (id_hash TEXT PRIMARY KEY, token_hash TEXT NOT NULL, expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS delivery_policies (project_id TEXT PRIMARY KEY, version INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, cycle INTEGER NOT NULL, active INTEGER NOT NULL, version INTEGER NOT NULL, body BLOB NOT NULL, UNIQUE(task_id,cycle));
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_active_task ON deliveries(task_id) WHERE active=1;
 CREATE TABLE IF NOT EXISTS delivery_steps (id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL, sequence INTEGER NOT NULL, fingerprint TEXT NOT NULL, body BLOB NOT NULL, UNIQUE(delivery_id,sequence));
 CREATE TABLE IF NOT EXISTS delivery_ci (delivery_id TEXT NOT NULL, stage TEXT NOT NULL, workflow_id INTEGER NOT NULL, run_id INTEGER NOT NULL, attempt INTEGER NOT NULL, body BLOB NOT NULL, PRIMARY KEY(delivery_id,stage,workflow_id,run_id,attempt));
 CREATE TABLE IF NOT EXISTS delivery_repairs (id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL, number INTEGER NOT NULL, phase TEXT NOT NULL, body BLOB NOT NULL, UNIQUE(delivery_id,number));
 CREATE TABLE IF NOT EXISTS delivery_queue (ticket INTEGER PRIMARY KEY AUTOINCREMENT, delivery_id TEXT NOT NULL UNIQUE, common_key TEXT NOT NULL, remote_key TEXT NOT NULL, owner INTEGER NOT NULL DEFAULT 0);
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_common_owner ON delivery_queue(common_key) WHERE owner=1;
 CREATE UNIQUE INDEX IF NOT EXISTS delivery_remote_owner ON delivery_queue(remote_key) WHERE owner=1;
 PRAGMA user_version=2;`

func TestSubagentSchema2MigrationPreservesExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema2DDL); err != nil {
		t.Fatal(err)
	}
	legacy := State{
		Projects:         map[string]Project{"p1": {ID: "p1", Name: "旧项目", Path: "/tmp/kaiwu-old", ConversationID: "c-old"}},
		Topics:           []Topic{{ID: "p1_other", ProjectID: "p1", Name: "其他", Protected: true}},
		Tasks:            map[string]Task{"t1": {ID: "t1", ProjectID: "p1", TopicID: "p1_other", Title: "旧卡片", Request: "旧需求", Status: "running", ConversationID: "c-old", ExecutionID: "x1", Profile: Profile{Provider: "test", Model: "fake"}}},
		Conversations:    map[string]Conversation{"c-old": {ID: "c-old", ProjectID: "p1", TaskID: "t1", SessionID: "kaiwu_c-old", RunState: "running", Profile: Profile{Provider: "test", Model: "fake"}, Messages: []Message{{ID: "m1", Role: "user", Text: "旧消息", Delivery: "accepted"}}, Rounds: []Round{{ID: "m1", Phase: "running", StartedAt: "2026-01-01T00:00:00Z", Tools: []ToolRecord{}}}}},
		Executions:       map[string]Execution{"x1": {ID: "x1", TaskID: "t1", ConversationID: "c-old", Phase: "running", StartedAt: "2026-01-01T00:00:00Z", Tools: []ToolRecord{}}},
		Deliveries:       map[string]DeliverySummary{},
		DeliveryPolicies: map[string]DeliveryPolicySummary{},
		TaskGit:          map[string]GitStatus{},
		Merges:           map[string]MergeOperation{},
		Profile:          Profile{Provider: "test", Model: "fake"},
		Revision:         7,
	}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(ProjectDeliveryPolicy{ProjectID: "p1", RequireAcceptance: false, MaxRepairs: 5, ActiveSeconds: 60, TriggerSeconds: 60, CISeconds: 60, Version: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO state VALUES (1,?,?)", 7, body); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO delivery_policies VALUES ('p1',4,?)", policy); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO login_sessions VALUES ('ih','th',?)", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot := store.Snapshot()
	if snapshot.Projects["p1"].Name != "旧项目" || snapshot.Tasks["t1"].Request != "旧需求" || snapshot.Executions["x1"].Phase != "interrupted" || snapshot.Tasks["t1"].Status != "interrupted" {
		t.Fatalf("migration lost legacy history or skipped normalization: %+v", snapshot)
	}
	if snapshot.Conversations["c-old"].Rounds[0].Phase != "interrupted" || snapshot.Conversations["c-old"].Messages[0].Delivery != "uncertain" {
		t.Fatalf("legacy conversation not normalized: %+v", snapshot.Conversations["c-old"])
	}
	if snapshot.Subagents == nil || len(snapshot.Subagents) != 0 {
		t.Fatalf("subagents projection not initialized: %+v", snapshot.Subagents)
	}
	if !store.ValidLogin("ih", "th") {
		t.Fatal("login history lost in migration")
	}
	policy2, err := store.DeliveryPolicy("p1")
	if err != nil || policy2.RequireAcceptance || policy2.MaxRepairs != 5 || policy2.Version != 4 {
		t.Fatalf("delivery policy lost in migration: %+v %v", policy2, err)
	}
	var version int
	if err = store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("schema not upgraded to 5: %d %v", version, err)
	}
	// The migrated database must accept the new public records.
	if _, err := store.CreateSubagentRun(SubagentSummary{ID: "run-migrated", ProjectID: "p1", ConversationID: "c-old", TaskID: "t1", RoundID: "m1", ParentSessionID: "kaiwu_c-old", Title: "迁移后", TaskSummary: "验证", Status: SubagentStatusStarting, Profile: Profile{Provider: "test", Model: "fake"}}, "迁移后的任务"); err != nil {
		t.Fatalf("migrated database rejected a subagent run: %v", err)
	}
}

func TestSubagentSchemaNewerThanSupportedRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path, Profile{}); err == nil {
		t.Fatal("newer database schema was accepted")
	}
}
