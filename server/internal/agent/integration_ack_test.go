package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// finishedReview makes the fixture look like a task whose newest round finished
// and which is waiting for the user, but was never confirmed.
func finishedReview(t *testing.T, m *Manager, task workbench.Task) {
	t.Helper()
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.RunState = "idle"
		c.Rounds = []workbench.Round{{
			ID: "round-1", Phase: "finished", StartedAt: "2026-10-24T09:00:00Z", FinishedAt: "2026-10-24T10:00:00Z",
			Report: &workbench.Report{Summary: "done", Checks: []workbench.ReportCheck{{Title: "go test", Claim: "passed", ToolID: "tool-1"}}},
			Tools:  []workbench.ToolRecord{{ID: "tool-1", Name: "bash", Result: "ok"}},
		}}
		st.Conversations[c.ID] = c
		v := st.Tasks[task.ID]
		v.Status = "needs_confirmation"
		st.Tasks[task.ID] = v
		x := st.Executions[v.ExecutionID]
		x.Phase = "awaiting_review"
		x.FinishedAt = "2026-10-24T10:00:00Z"
		st.Executions[x.ID] = x
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

// A local review binds the newest finished round and admits the merge without a
// separate confirm_result, then integrates locally.
func TestMergeReviewAcknowledgesAndIntegratesLocally(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	finishedReview(t, m, task)
	if e := os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("feature"), 0600); e != nil {
		t.Fatal(e)
	}
	review, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !review.CanMerge || review.Mode != workbench.MergeModeLocal || review.RoundID != "round-1" {
		t.Fatal(review, e)
	}
	req := workbench.MergeRequest{OperationID: "merge-review", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"}
	op, replay, e := m.StartMerge(req)
	if e != nil || replay {
		t.Fatal(op, e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "succeeded" || op.Mode != workbench.MergeModeLocal || op.RoundID != "round-1" {
		t.Fatal(op)
	}
	st := m.store.Snapshot()
	c := st.Conversations[task.ConversationID]
	if c.Rounds[len(c.Rounds)-1].Phase != "confirmed" || st.Tasks[task.ID].Status != "completed" {
		t.Fatalf("not acknowledged: %#v", c.Rounds[len(c.Rounds)-1].Phase)
	}
	if b, e := os.ReadFile(filepath.Join(source, "feature.txt")); e != nil || string(b) != "feature" {
		t.Fatal("not integrated", e)
	}
	// Evidence stays exactly as the runner reported it.
	r := c.Rounds[len(c.Rounds)-1]
	if len(r.Report.Checks) != 1 || r.Report.Checks[0].Claim != "passed" || len(r.Tools) != 1 {
		t.Fatalf("evidence rewritten: %#v", r)
	}
	if _, replay, e := m.StartMerge(req); e != nil || !replay {
		t.Fatalf("lost idempotency: %v %v", replay, e)
	}
}

// Without a review the legacy path must still never acknowledge anything.
func TestPreviewMergeReviewRequiresFinishedRound(t *testing.T) {
	m, task, _, _ := integrationFixture(t)
	if _, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"}); e == nil {
		t.Fatal("review of a task with no finished round accepted")
	}
	finishedReview(t, m, task)
	if _, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "stale-round"}); e == nil {
		t.Fatal("stale round accepted")
	}
	for _, mode := range []string{workbench.MergeModePush, "", "remote"} {
		if _, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: mode, RoundID: "round-1"}); e == nil {
			t.Fatalf("mode %q accepted", mode)
		}
	}
	if st := m.store.Snapshot(); st.Tasks[task.ID].Status == "completed" {
		t.Fatal("rejected review acknowledged")
	}
}

// StartMerge must refuse a review proof bound to another round or mode, and a
// push review must not run any worker.
func TestStartMergeRejectsMismatchedOrPushReview(t *testing.T) {
	m, task, x, _ := integrationFixture(t)
	finishedReview(t, m, task)
	_ = os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("feature"), 0600)
	review, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !review.CanMerge {
		t.Fatal(review, e)
	}
	base := workbench.MergeRequest{OperationID: "merge-mismatch", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"}
	for _, mutate := range []func(*workbench.MergeRequest){
		func(r *workbench.MergeRequest) { r.RoundID = "round-2" },
		func(r *workbench.MergeRequest) { r.Mode = workbench.MergeModePush },
	} {
		req := base
		mutate(&req)
		if _, _, e := m.StartMerge(req); e == nil {
			t.Fatal("mismatched review accepted", req)
		}
	}
	if st := m.store.Snapshot(); len(st.Merges) != 0 || st.Tasks[task.ID].Status == "completed" {
		t.Fatal("rejected review changed state")
	}
	// The legacy preview keeps working and stays unacknowledged.
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !p.CanMerge || p.Mode != "" || p.RoundID != "" {
		t.Fatal(p, e)
	}
	if st := m.store.Snapshot(); st.Conversations[task.ConversationID].Rounds[0].Phase != "finished" {
		t.Fatal("legacy preview acknowledged")
	}
}

// A new round started after the review must invalidate it instead of letting
// the merge proceed on stale evidence.
func TestReviewedMergeRejectsNewRoundAfterReview(t *testing.T) {
	m, task, x, _ := integrationFixture(t)
	finishedReview(t, m, task)
	_ = os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("feature"), 0600)
	review, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !review.CanMerge {
		t.Fatal(review, e)
	}
	if e := m.store.Update(func(st *workbench.State) error {
		workbench.BeginRound(st, task.ConversationID, "round-2")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "merge-new-round", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e == nil {
		op = waitMerge(t, m, op.ID)
		if op.Phase == "succeeded" {
			t.Fatal("stale review merged")
		}
	}
	st := m.store.Snapshot()
	if st.Tasks[task.ID].Status == "completed" || st.Conversations[task.ConversationID].Rounds[0].Phase == "confirmed" {
		t.Fatal("stale review acknowledged")
	}
}

// A legacy task without any Round must be reviewable with its exact persisted
// token and acknowledged in the same write that queues the merge.
func TestMergeReviewAcknowledgesLegacyTask(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	token := "legacy:" + x.ID + ":2026-10-24T10:00:00Z"
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.RunState, c.Rounds = "idle", nil
		st.Conversations[c.ID] = c
		v := st.Tasks[task.ID]
		v.Status = "needs_confirmation"
		st.Tasks[task.ID] = v
		y := st.Executions[v.ExecutionID]
		y.Phase, y.FinishedAt = "awaiting_review", "2026-10-24T10:00:00Z"
		st.Executions[y.ID] = y
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(x.Workspace, "legacy.txt"), []byte("legacy"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "legacy:" + x.ID + ":2026-10-24T09:00:00Z"}); e == nil {
		t.Fatal("wrong legacy token accepted")
	}
	review, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: token})
	if e != nil || !review.CanMerge {
		t.Fatal(review, e)
	}
	req := workbench.MergeRequest{OperationID: "merge-legacy-review", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "legacy", Mode: workbench.MergeModeLocal, RoundID: token}
	op, _, e := m.StartMerge(req)
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatal(op)
	}
	st := m.store.Snapshot()
	c := st.Conversations[task.ConversationID]
	if len(c.Rounds) != 1 || c.Rounds[0].ID != token || c.Rounds[0].Phase != "confirmed" {
		t.Fatalf("legacy round not synthesized: %#v", c.Rounds)
	}
	if st.Tasks[task.ID].Status != "completed" || st.Executions[x.ID].Phase != "confirmed" {
		t.Fatal("legacy task not confirmed")
	}
	if b, e := os.ReadFile(filepath.Join(source, "legacy.txt")); e != nil || string(b) != "legacy" {
		t.Fatal("not integrated", e)
	}
}
