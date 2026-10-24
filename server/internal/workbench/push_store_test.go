package workbench

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func newPushStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func pushState() (State, Task, MergeReview) {
	st := State{Projects: map[string]Project{}, Tasks: map[string]Task{}, Conversations: map[string]Conversation{}, Executions: map[string]Execution{}, Merges: map[string]MergeOperation{}}
	st.Projects["p"] = Project{ID: "p", Path: "/tmp/p", DefaultBranch: "main"}
	st.Tasks["t"] = Task{ID: "t", ProjectID: "p", ConversationID: "c", ExecutionID: "x", Status: "needs_confirmation"}
	st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle"}
	st.Executions["x"] = Execution{ID: "x", TaskID: "t", ConversationID: "c", Workspace: "/tmp/p/t", BaseSHA: strings.Repeat("a", 40), Phase: "awaiting_review", FinishedAt: "2026-10-24T10:00:00Z"}
	task := st.Tasks["t"]
	task.Status = "needs_confirmation"
	st.Tasks["t"] = task
	c := st.Conversations[task.ConversationID]
	c.RunState = "idle"
	c.Rounds = []Round{{ID: "round-1", Phase: "finished", StartedAt: "2026-10-24T09:00:00Z", FinishedAt: "2026-10-24T10:00:00Z"}}
	st.Conversations[c.ID] = c
	review := MergeReview{
		MergePreview:      MergePreview{ID: "preview-1", ProjectID: task.ProjectID, TaskID: task.ID, MainSHA: strings.Repeat("a", 40), TaskSHA: strings.Repeat("b", 40), CanMerge: false},
		RemoteName:        "origin",
		RemoteURL:         "https://github.com/owner/repo.git",
		MainRef:           "refs/heads/main",
		ObservedRemoteSHA: strings.Repeat("a", 40),
		LocalMainSHA:      strings.Repeat("a", 40),
		Uploads:           []MergeUpload{{SHA: strings.Repeat("b", 40), Subject: "feature"}},
		Pending:           []GitFile{},
		AlreadyMerged:     true,
		PushOnly:          true,
		CanPush:           true,
		SafetyProof:       "owner+main-ready+private-scope+worktree+committed-scope",
		CredentialRef:     "server_github_https",
		ReviewFingerprint: "review-fp",
	}
	review.Mode, review.RoundID = MergeModePush, "round-1"
	review.ExpiresAt = "2999-01-01T00:00:00Z"
	return st, task, review
}

func pushRequest(task Task) MergeRequest {
	return MergeRequest{OperationID: "op-1", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: "preview-1", CommitMessage: "feature", Mode: MergeModePush, RoundID: "round-1"}
}

// Admission persists the frozen review, the acknowledgement and the queued
// operation in one revision, so a crash cannot leave a half-authorised push.
func TestBeginMergeReviewPersistsFreezeAtomically(t *testing.T) {
	s := newPushStore(t)
	st, task, review := pushState()
	if e := s.Update(func(v *State) error { *v = st; return nil }); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot().Revision
	op, replay, e := s.BeginMergeReview(pushRequest(task), review)
	if e != nil || replay {
		t.Fatal(op, replay, e)
	}
	after := s.Snapshot()
	if after.Revision != before+1 {
		t.Fatalf("not one revision: %d -> %d", before, after.Revision)
	}
	got := after.Merges["op-1"]
	if got.Phase != "queued" || got.Push == nil || got.Push.RemoteURL != review.RemoteURL || got.Push.ObservedSHA != review.ObservedRemoteSHA {
		t.Fatalf("freeze: %#v", got.Push)
	}
	if after.Conversations[task.ConversationID].Rounds[0].Phase != "confirmed" {
		t.Fatal("round not acknowledged")
	}
	if r, e := s.Operation("op-1"); e != nil || r.CreatedID != "op-1" {
		t.Fatalf("no receipt: %v %v", r, e)
	}
	// Replay returns the original without a second acknowledgement.
	again, replay, e := s.BeginMergeReview(pushRequest(task), review)
	if e != nil || !replay || again.ID != "op-1" || s.Snapshot().Revision != after.Revision {
		t.Fatalf("replay: %#v %v %v", again, replay, e)
	}
	changed := pushRequest(task)
	changed.CommitMessage = "different"
	if _, _, e := s.BeginMergeReview(changed, review); e == nil {
		t.Fatal("different arguments accepted")
	}
}

// A push review that is not pushable is refused before anything is queued.
func TestBeginMergeReviewRefusesBlockedReview(t *testing.T) {
	s := newPushStore(t)
	st, task, review := pushState()
	if e := s.Update(func(v *State) error { *v = st; return nil }); e != nil {
		t.Fatal(e)
	}
	review.CanPush = false
	review.PushReason = "远程主分支已有本地不包含的提交"
	if _, _, e := s.BeginMergeReview(pushRequest(task), review); e == nil {
		t.Fatal("blocked review accepted")
	}
	if n := len(s.Snapshot().Merges); n != 0 {
		t.Fatalf("queued %d", n)
	}
}

// The local mode keeps its frozen admission and never gains push fields.
func TestBeginMergeLocalHasNoPushFreeze(t *testing.T) {
	s := newPushStore(t)
	st, task, _ := pushState()
	if e := s.Update(func(v *State) error { *v = st; return nil }); e != nil {
		t.Fatal(e)
	}
	proof := MergePreview{ID: "preview-local", ProjectID: task.ProjectID, TaskID: task.ID, MainSHA: strings.Repeat("a", 40), TaskSHA: strings.Repeat("b", 40), CanMerge: true, Fingerprint: "fp"}
	proof.Mode, proof.RoundID = MergeModeLocal, "round-1"
	req := MergeRequest{OperationID: "local-1", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: "preview-local", CommitMessage: "feature", Mode: MergeModeLocal, RoundID: "round-1"}
	op, _, e := s.BeginMerge(req, proof)
	if e != nil {
		t.Fatal(e)
	}
	if op.Push != nil || op.IntendedSHA != "" || op.RemoteAfterSHA != "" {
		t.Fatalf("local op carries remote facts: %#v", op)
	}
}

// Legacy merge JSON and its fingerprint stay byte-identical: new fields are
// omitted when empty and the legacy request keeps working without a mode.
func TestLegacyMergeJSONHasNoPushFields(t *testing.T) {
	s := newPushStore(t)
	st, task, _ := pushState()
	if e := s.Update(func(v *State) error { *v = st; return nil }); e != nil {
		t.Fatal(e)
	}
	proof := MergePreview{ID: "preview-legacy", ProjectID: task.ProjectID, TaskID: task.ID, MainSHA: strings.Repeat("a", 40), TaskSHA: strings.Repeat("b", 40), CanMerge: true, Fingerprint: "fp"}
	proof.Mode, proof.RoundID = "", ""
	req := MergeRequest{OperationID: "legacy-1", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: "preview-legacy", CommitMessage: "feature"}
	op, _, e := s.BeginMerge(req, proof)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(op)
	if e != nil {
		t.Fatal(e)
	}
	for _, forbidden := range []string{"push", "intendedSHA", "remoteAfterSHA", "ciStatus", "mode", "roundId"} {
		if strings.Contains(string(b), `"`+forbidden+`"`) {
			t.Fatalf("legacy JSON gained %q: %s", forbidden, b)
		}
	}
	if op.Fingerprint != "fp" || op.Preview.ID != "preview-legacy" {
		t.Fatalf("legacy op changed: %#v", op)
	}
}
