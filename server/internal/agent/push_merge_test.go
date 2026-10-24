package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// pushConn is a narrow test transport: the frozen production URL stays legal,
// but commands run against a temporary bare repository so the push itself is a
// real Git write with real ref verification.
type pushConn struct {
	mu        sync.Mutex
	bare      string
	pushes    int
	before    func()
	observeOK bool
	pushFail  bool
	// failObserveAfterPush makes the first push reach the remote but hides the
	// result, and pushNoRun refuses the push without writing it.
	failObserveAfterPush bool
	pushNoRun            bool
}

func (c *pushConn) transport() gitops.RemoteTransport {
	return gitops.RemoteTransportFunc(func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
		track := gitops.LeaseTrack(lease)
		// This transport fabricates some answers without spawning Git; only those
		// paths prove "no child", and they must say so instead of guessing later.
		noProcess := func() {
			if f, ok := lease.(interface{ NoProcess() }); ok {
				f.NoProcess()
			}
		}
		if !strings.HasPrefix(target.URL, "https://github.com/") {
			return "", errors.New("unexpected url")
		}
		real := make([]string, 0, len(args))
		for _, a := range args {
			if a == target.URL {
				a = c.bare
			}
			real = append(real, a)
		}
		if args[0] == "push" {
			c.mu.Lock()
			c.pushes++
			hook := c.before
			fail := c.pushFail
			c.mu.Unlock()
			noRun := c.pushNoRun
			if hook != nil {
				hook()
			}
			if noRun {
				noProcess()
				return "", errors.New("push: connection reset by peer")
			}
			out, e := gitops.Run(ctx, dir, track, real...)
			if fail {
				return string(out), errors.New("push: connection reset by peer")
			}
			return string(out), e
		}
		c.mu.Lock()
		ok := c.observeOK && !(c.failObserveAfterPush && c.pushes > 0)
		c.mu.Unlock()
		if !ok {
			noProcess()
			return "", errors.New("ls-remote: connection reset by peer")
		}
		out, e := gitops.Run(ctx, dir, track, real...)
		return out, e
	})
}

func (c *pushConn) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.pushes }

func pushFixture(t *testing.T) (*Manager, workbench.Task, workbench.Execution, string, *pushConn) {
	t.Helper()
	m, task, x, source := reviewManager(t)
	conn := &pushConn{bare: reviewBare(t, source, "HEAD"), observeOK: true}
	resolver, e := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) { return "token", true }})
	if e != nil {
		t.Fatal(e)
	}
	m.cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver, Transport: conn.transport()})
	return m, task, x, source, conn
}

func pushReview(t *testing.T, m *Manager, task workbench.Task) workbench.MergeReview {
	t.Helper()
	review, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil || !review.CanPush {
		t.Fatal(review, e)
	}
	return review
}

func startPush(t *testing.T, m *Manager, task workbench.Task, review workbench.MergeReview, id string) workbench.MergeOperation {
	t.Helper()
	op, replay, e := m.StartMerge(workbench.MergeRequest{OperationID: id, ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil || replay {
		t.Fatal(op, replay, e)
	}
	return waitMerge(t, m, op.ID)
}

func bareRef(t *testing.T, bare, ref string) string {
	t.Helper()
	out, e := exec.Command("git", "-C", bare, "rev-parse", "--verify", ref).Output()
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// A reviewed push really writes the exact commit to the temporary remote.
func TestPushMergeWritesExactRef(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	// Dirty reviewed work: the push must commit it, merge it and then push.
	if e := os.WriteFile(filepath.Join(x.Workspace, "more.txt"), []byte("more"), 0600); e != nil {
		t.Fatal(e)
	}
	review := pushReview(t, m, task)
	op := startPush(t, m, task, review, "push-dirty")
	if op.Phase != "succeeded" || op.RemoteAfterSHA == "" || op.CIStatus != "not_checked" {
		t.Fatalf("op: %#v", op)
	}
	branch := m.store.Snapshot().Projects[task.ProjectID].DefaultBranch
	if got := bareRef(t, conn.bare, "refs/heads/"+branch); got != op.IntendedSHA || got != op.RemoteAfterSHA {
		t.Fatalf("remote ref %q != intended %q", got, op.IntendedSHA)
	}
	if op.TaskAfterSHA == "" || op.MainAfterSHA != op.IntendedSHA {
		t.Fatalf("missing local facts: %#v", op)
	}
	// The pushed commit is the reviewed merge result, not a rewritten branch.
	if !strings.Contains(workspaceGit(t, source, "log", "-1", "--format=%s", op.IntendedSHA), "Merge task") &&
		workspaceGit(t, source, "rev-parse", "HEAD") != op.IntendedSHA {
		t.Fatalf("unexpected pushed commit")
	}
	if _, e := os.Stat(x.Workspace); e != nil {
		t.Fatal("worktree removed")
	}
	if conn.count() != 1 {
		t.Fatalf("pushes=%d", conn.count())
	}
}

// An already merged task is not re-committed or re-merged; only the push runs.
func TestPushMergeAlreadyMergedDoesNotRewriteLocal(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	// Merge locally first so the reviewed push has nothing left to commit or
	// merge, which is the already-merged push-only case.
	local, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !local.CanMerge {
		t.Fatal(local, e)
	}
	merged, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "local-first", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: local.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil {
		t.Fatal(e)
	}
	if merged = waitMerge(t, m, merged.ID); merged.Phase != "succeeded" {
		t.Fatalf("local merge: %#v", merged)
	}
	review := pushReview(t, m, task)
	if !review.AlreadyMerged || !review.PushOnly {
		t.Fatalf("expected push-only: %#v", review)
	}
	mainBefore := workspaceGit(t, source, "rev-parse", "HEAD")
	op := startPush(t, m, task, review, "push-only")
	if op.Phase != "succeeded" || conn.count() != 1 {
		t.Fatalf("op %#v pushes %d", op, conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); got != mainBefore {
		t.Fatalf("local main rewritten: %s -> %s", mainBefore, got)
	}
	if op.MainAfterSHA != mainBefore {
		t.Fatalf("main fact: %#v", op)
	}
}

// A remote that moved after the review blocks the push before any local effect.
func TestPushMergeRejectsStaleRemote(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	if e := os.WriteFile(filepath.Join(x.Workspace, "more.txt"), []byte("more"), 0600); e != nil {
		t.Fatal(e)
	}
	review := pushReview(t, m, task)
	// Somebody else advances the remote main.
	other := filepath.Join(t.TempDir(), "other")
	if out, e := exec.Command("git", "clone", "-q", source, other).CombinedOutput(); e != nil {
		t.Fatalf("clone: %s %v", out, e)
	}
	os.WriteFile(filepath.Join(other, "external.txt"), []byte("x"), 0600)
	workspaceGit(t, other, "add", ".")
	workspaceGit(t, other, "-c", "user.name=F", "-c", "user.email=f@example.invalid", "commit", "-q", "-m", "external")
	workspaceGit(t, other, "push", "-q", conn.bare, "HEAD:refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch)
	op := startPush(t, m, task, review, "push-stale")
	if op.Phase == "succeeded" || conn.count() != 0 {
		t.Fatalf("stale remote accepted: %#v pushes=%d", op, conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); got != review.LocalMainSHA {
		t.Fatal("local main advanced despite refusal")
	}
	if workspaceGit(t, x.Workspace, "rev-parse", "HEAD") != review.TaskSHA {
		t.Fatal("task commit rewritten despite refusal")
	}
}

// A push that was written but whose receipt was lost is never retried: only the
// remote observation decides, and recovery adds no second push.
func TestPushMergeLostReceiptObservesOnly(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	// The push reaches the remote, then every answer to the client is lost.
	conn.mu.Lock()
	conn.pushFail = true
	conn.failObserveAfterPush = true
	conn.mu.Unlock()
	op := startPush(t, m, task, review, "push-lost")
	if op.Phase != "needs_check" {
		t.Fatalf("unknown result must not be success/failure: %#v", op)
	}
	if conn.count() != 1 {
		t.Fatalf("pushes=%d", conn.count())
	}
	if got := bareRef(t, conn.bare, "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch); got != op.IntendedSHA {
		t.Fatalf("remote did not receive intended commit: %q", got)
	}
	// Local completion is preserved even though the remote result is unknown.
	if op.MainAfterSHA == "" || workspaceGit(t, source, "rev-parse", "HEAD") != op.MainAfterSHA {
		t.Fatalf("local merge lost: %#v", op)
	}
	// Recovery only observes; the push count must not grow and nothing restarts.
	conn.mu.Lock()
	conn.failObserveAfterPush = false
	conn.mu.Unlock()
	if e := m.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := m.store.Snapshot().Merges[op.ID]
	if after.Phase != "succeeded" || after.RemoteAfterSHA != op.IntendedSHA {
		t.Fatalf("recovery did not prove success: %#v", after)
	}
	if conn.count() != 1 {
		t.Fatalf("recovery pushed again: %d", conn.count())
	}
	if after.CIStatus != "not_checked" {
		t.Fatalf("CI claimed: %#v", after)
	}
}

// A push that never reached the remote stays unknown; an unchanged remote SHA is
// not evidence of success.
func TestPushMergeUnwrittenStaysNeedsCheck(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	conn.mu.Lock()
	conn.pushFail = true
	conn.pushNoRun = true
	conn.failObserveAfterPush = true
	conn.mu.Unlock()
	op := startPush(t, m, task, review, "push-nowrite")
	if op.Phase != "needs_check" || conn.count() != 1 {
		t.Fatalf("op %#v pushes %d", op, conn.count())
	}
	conn.mu.Lock()
	conn.failObserveAfterPush = false
	conn.mu.Unlock()
	if e := m.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := m.store.Snapshot().Merges[op.ID]
	if after.Phase != "needs_check" || after.RemoteAfterSHA != "" {
		t.Fatalf("unproven push treated as success: %#v", after)
	}
	if conn.count() != 1 {
		t.Fatalf("recovery re-pushed: %d", conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); op.MainAfterSHA == "" || got != op.MainAfterSHA {
		t.Fatalf("local merge lost: %#v", op)
	}
}

// Same operation id replays the original result; different arguments are refused.
func TestPushMergeReplayAndArgumentConflict(t *testing.T) {
	m, task, _, _, conn := pushFixture(t)
	review := pushReview(t, m, task)
	req := workbench.MergeRequest{OperationID: "push-replay", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"}
	op, replay, e := m.StartMerge(req)
	if e != nil || replay {
		t.Fatal(op, replay, e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "succeeded" || conn.count() != 1 {
		t.Fatalf("%#v", op)
	}
	again, replay, e := m.StartMerge(req)
	if e != nil || !replay || again.ID != op.ID || conn.count() != 1 {
		t.Fatalf("replay failed: %v %v %v", again.Phase, replay, e)
	}
	changed := req
	changed.CommitMessage = "different"
	if _, _, e := m.StartMerge(changed); e == nil {
		t.Fatal("varying arguments accepted for the same operation id")
	}
	if conn.count() != 1 {
		t.Fatalf("conflict pushed: %d", conn.count())
	}
}

// A push review can never be confirmed into a push operation once the frozen
// review is unavailable: the client only supplies identifiers.
func TestPushMergeRequiresReviewBinding(t *testing.T) {
	m, task, _, _, conn := pushFixture(t)
	review := pushReview(t, m, task)
	_, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "push-noreview", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-2"})
	if e == nil {
		t.Fatal("round mismatch accepted")
	}
	m.mu.Lock()
	delete(m.reviews, review.ID)
	m.mu.Unlock()
	_, _, e = m.StartMerge(workbench.MergeRequest{OperationID: "push-noreview", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e == nil || conn.count() != 0 {
		t.Fatalf("missing review accepted: %v pushes=%d", e, conn.count())
	}
}

// A local push never touches the remote: no pushes, remote unchanged.
func TestLocalMergePerformsNoRemoteWrite(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	if e := os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	before := bareRef(t, conn.bare, "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch)
	p, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !p.CanMerge {
		t.Fatal(p, e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "local-only", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil {
		t.Fatal(e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "succeeded" || conn.count() != 0 || op.RemoteAfterSHA != "" {
		t.Fatalf("%#v pushes=%d", op, conn.count())
	}
	if got := bareRef(t, conn.bare, "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch); got != before {
		t.Fatal("local merge wrote the remote")
	}
	if workspaceGit(t, source, "rev-parse", "HEAD") != op.MainAfterSHA {
		t.Fatal("local main not merged")
	}
}

// When the store refuses the admission persist, nothing is queued and no Git
// write or push happens.
func TestPushAdmissionPersistFailureHasNoSideEffect(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	mainBefore := workspaceGit(t, source, "rev-parse", "HEAD")
	_ = m.store.Close()
	_, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "push-nostore", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e == nil {
		t.Fatal("admission succeeded without persistence")
	}
	if conn.count() != 0 {
		t.Fatalf("pushed without admission: %d", conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "HEAD"); got != mainBefore {
		t.Fatal("local main changed without admission")
	}
	_ = time.Now()
}
