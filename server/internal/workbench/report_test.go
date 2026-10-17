package workbench

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredReportPreservesOriginalAndRejectsInvalid(t *testing.T) {
	text := `已实现，正在核对。<kaiwu-report>{"summary":"登录恢复已实现","stage":"verifying","plan":["保存登录","验证恢复"],"results":["刷新恢复"],"checks":[{"title":"回归","claim":"passed","toolId":"tool-1"}],"blockers":[],"next":["核对退出"],"question":""}</kaiwu-report>`
	r, visible := ParseReport(text)
	if r == nil || r.Summary != "登录恢复已实现" || visible != "已实现，正在核对。" {
		t.Fatal(r, visible)
	}
	for _, bad := range []string{`<kaiwu-report>{"summary":"x","unknown":true}</kaiwu-report>`, `<kaiwu-report>{`, strings.Replace(text, "verifying", "invented", 1), strings.Replace(text, "登录恢复已实现", strings.Repeat("x", 2001), 1)} {
		if r, v := ParseReport(bad); r != nil || v != bad {
			t.Fatal("invalid report swallowed original", r, v)
		}
	}
}
func TestRoundEvidenceNeverTreatsClaimAsVerification(t *testing.T) {
	r := Round{ID: "r", Report: &Report{Checks: []ReportCheck{{Title: "test", Claim: "passed", ToolID: "foreign"}}}, Tools: []ToolRecord{{ID: "actual", Name: "bash", Result: "done"}}}
	if Evidence(r, r.Report.Checks[0]) != "missing" {
		t.Fatal("foreign evidence accepted")
	}
	r.Report.Checks[0].ToolID = "actual"
	r.Tools[0].Failed = true
	if Evidence(r, r.Report.Checks[0]) != "failed" {
		t.Fatal("AI claim overrode failed execution")
	}
}
func TestRoundKeepsPreviousResultAndConfirmationRequiresLatestFinishedRound(t *testing.T) {
	st := State{Tasks: map[string]Task{"t": {ID: "t", ProjectID: "p", ConversationID: "c", Status: "needs_confirmation", ExecutionID: "x"}}, Conversations: map[string]Conversation{"c": {ID: "c", RunState: "idle"}}, Executions: map[string]Execution{"x": {ID: "x", Phase: "awaiting_review"}}}
	BeginRound(&st, "c", "one")
	UpdateRound(&st, "c", "one", func(r *Round) { r.Phase = "finished"; r.Report = &Report{Summary: "previous"} })
	BeginRound(&st, "c", "two")
	cmd := Command{TaskID: "t", ProjectID: "p", ConversationID: "c", RoundID: "one"}
	if _, e := ConfirmResult(&st, cmd); e == nil {
		t.Fatal("old round accepted")
	}
	cmd.RoundID = "two"
	if _, e := ConfirmResult(&st, cmd); e == nil {
		t.Fatal("unfinished round accepted")
	}
	UpdateRound(&st, "c", "two", func(r *Round) { r.Phase = "finished" })
	if _, e := ConfirmResult(&st, cmd); e != nil {
		t.Fatal(e)
	}
	if st.Conversations["c"].Rounds[0].Report.Summary != "previous" || st.Tasks["t"].Status != "completed" {
		t.Fatal(st)
	}
}

func TestRoundRestartPreservesReportAndEndsInterruptedRound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Update(func(st *State) error {
		st.Conversations["c"] = Conversation{ID: "c", RunState: "running"}
		BeginRound(st, "c", "r")
		UpdateRound(st, "c", "r", func(r *Round) { r.Report = &Report{Summary: "last known"} })
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := s.Snapshot().Conversations["c"].Rounds[0]
	if r.Phase != "interrupted" || r.FinishedAt == "" || r.Report.Summary != "last known" {
		t.Fatal("invalid restored round", r)
	}
}

func TestStoredFinishedResultCanBeAcceptedWithoutRestoringCLI(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			st := State{Tasks: map[string]Task{"t": {ID: "t", ProjectID: "p", ConversationID: "c", ExecutionID: "x", Status: "needs_confirmation"}}, Conversations: map[string]Conversation{"c": {ID: "c", RunState: "interrupted"}}, Executions: map[string]Execution{"x": {ID: "x", Phase: "awaiting_review", Summary: "done", FinishedAt: "2026-10-14T00:00:00Z"}}}
			token := "r"
			if legacy {
				token = "legacy:x:2026-10-14T00:00:00Z"
			} else {
				BeginRound(&st, "c", "r")
				UpdateRound(&st, "c", "r", func(r *Round) { r.Phase = "finished" })
			}
			if _, e := ConfirmResult(&st, Command{ProjectID: "p", TaskID: "t", ConversationID: "c", RoundID: token}); e != nil {
				t.Fatal(e)
			}
			if st.Tasks["t"].Status != "completed" || st.Conversations["c"].RunState != "interrupted" {
				t.Fatal("acceptance must not start CLI", st)
			}
		})
	}
}
func TestLegacyInterruptedStateNormalizesOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Update(func(st *State) error {
		st.Tasks["t"] = Task{ID: "t", ConversationID: "c", ExecutionID: "x", Status: "needs_confirmation"}
		st.Conversations["c"] = Conversation{ID: "c", TaskID: "t", RunState: "interrupted"}
		st.Executions["x"] = Execution{ID: "x", TaskID: "t", ConversationID: "c", Phase: "awaiting_review", Tools: []ToolRecord{{ID: "old", Name: "write", Result: "saved"}}}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	st := s.Snapshot()
	if st.Tasks["t"].Status != "interrupted" || len(st.Executions["x"].Tools) != 1 {
		t.Fatal("legacy interrupted task remains misleading", st.Tasks["t"])
	}
}
