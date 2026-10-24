package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Unreviewed new work after the review is refused before any local effect.
func TestPushRefusesUnreviewedPendingWork(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	mainBefore := workspaceGit(t, source, "rev-parse", "HEAD")
	taskBefore := review.TaskSHA
	if e := os.WriteFile(filepath.Join(x.Workspace, "sneaky.txt"), []byte("unreviewed"), 0600); e != nil {
		t.Fatal(e)
	}
	op := startPush(t, m, task, review, "push-sneaky")
	if op.Phase == "succeeded" || conn.count() != 0 {
		t.Fatalf("unreviewed work pushed: %#v pushes=%d", op, conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); got != mainBefore {
		t.Fatal("local main advanced")
	}
	if got := workspaceGit(t, x.Workspace, "rev-parse", "HEAD"); got != taskBefore {
		t.Fatalf("unreviewed commit created: %s", got)
	}
	if op.MainAfterSHA != "" {
		t.Fatal("main fact recorded without a merge")
	}
}

// Main commits made after the review are not silently pushed.
func TestPushRefusesExtraMainCommits(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	if e := os.WriteFile(filepath.Join(x.Workspace, "more.txt"), []byte("more"), 0600); e != nil {
		t.Fatal(e)
	}
	review := pushReview(t, m, task)
	os.WriteFile(filepath.Join(source, "unrelated.txt"), []byte("unrelated"), 0600)
	workspaceGit(t, source, "add", "unrelated.txt")
	workspaceGit(t, source, "-c", "user.name=F", "-c", "user.email=f@example.invalid", "commit", "-q", "-m", "unrelated")
	op := startPush(t, m, task, review, "push-extra-main")
	if op.Phase == "succeeded" || conn.count() != 0 {
		t.Fatalf("extra main commits pushed: %#v pushes=%d", op, conn.count())
	}
	// The unrelated local commit stays local and is never advertised as uploaded.
	if got := bareRef(t, conn.bare, "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch); got != review.ObservedRemoteSHA {
		t.Fatalf("remote changed: %q", got)
	}
}

// While another writer owns the task lease, admission still queues but the push
// must not start a Git write; it fails closed and keeps the remote untouched.
func TestPushBlockedByActiveWriter(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	if e := os.WriteFile(filepath.Join(x.Workspace, "more.txt"), []byte("more"), 0600); e != nil {
		t.Fatal(e)
	}
	review := pushReview(t, m, task)
	mainBefore := workspaceGit(t, source, "rev-parse", "HEAD")
	held, e := acquireExecutionLease(x.Workspace)
	if e != nil {
		t.Fatal(e)
	}
	defer held.release(true)
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "push-busy", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil {
		t.Fatal(e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase == "succeeded" || conn.count() != 0 {
		t.Fatalf("concurrent writer bypassed: %#v pushes=%d", op, conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); got != mainBefore {
		t.Fatal("main advanced while a writer was active")
	}
}

// Concurrent confirms of one operation id produce exactly one push.
func TestPushSameOperationConcurrency(t *testing.T) {
	m, task, _, _, conn := pushFixture(t)
	review := pushReview(t, m, task)
	req := workbench.MergeRequest{OperationID: "push-race", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"}
	type res struct {
		op     workbench.MergeOperation
		replay bool
		err    error
	}
	out := make(chan res, 2)
	for i := 0; i < 2; i++ {
		go func() {
			op, replay, e := m.StartMerge(req)
			out <- res{op, replay, e}
		}()
	}
	first, second := <-out, <-out
	if first.err != nil || second.err != nil {
		t.Fatal(first.err, second.err)
	}
	if first.op.ID != second.op.ID || first.replay == second.replay {
		t.Fatalf("not serialised: %#v %#v", first, second)
	}
	if op := waitMerge(t, m, first.op.ID); op.Phase != "succeeded" {
		t.Fatal(op)
	}
	if conn.count() != 1 {
		t.Fatalf("pushes=%d", conn.count())
	}
	if n := len(m.store.Snapshot().Merges); n != 1 {
		t.Fatalf("operations=%d", n)
	}
}

// Restart re-runs recovery over persisted state only; a needs_check push whose
// remote now shows an unknown commit is never promoted to success.
func TestRecoveryKeepsUnknownRemoteAsNeedsCheck(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	conn.mu.Lock()
	conn.pushFail = true
	conn.pushNoRun = true
	conn.failObserveAfterPush = true
	conn.mu.Unlock()
	op := startPush(t, m, task, review, "push-unknown-sha")
	if op.Phase != "needs_check" {
		t.Fatalf("%#v", op)
	}
	// Somebody else's commit becomes the remote head; it exists only remotely.
	other := filepath.Join(t.TempDir(), "other")
	workspaceGit(t, "", "clone", "-q", source, other)
	os.WriteFile(filepath.Join(other, "other.txt"), []byte("other"), 0600)
	workspaceGit(t, other, "add", ".")
	workspaceGit(t, other, "-c", "user.name=F", "-c", "user.email=f@example.invalid", "commit", "-q", "-m", "other")
	workspaceGit(t, other, "push", "-q", conn.bare, "HEAD:refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch)
	conn.mu.Lock()
	conn.failObserveAfterPush = false
	conn.mu.Unlock()
	if e := m.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := m.store.Snapshot().Merges[op.ID]
	if after.Phase != "needs_check" || after.RemoteAfterSHA != "" || conn.count() != 1 {
		t.Fatalf("unknown remote promoted: %#v pushes=%d", after, conn.count())
	}
	if op.MainAfterSHA == "" || after.MainAfterSHA != op.MainAfterSHA {
		t.Fatal("local merge fact lost in recovery")
	}
}
