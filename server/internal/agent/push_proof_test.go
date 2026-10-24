package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// craftCommit builds a commit object with an arbitrary parent list and tree, so
// a candidate can claim the reviewed parents while carrying unreviewed content.
func craftCommit(t *testing.T, dir string, parents []string, files map[string]string) string {
	t.Helper()
	index := filepath.Join(t.TempDir(), "index")
	env := append(os.Environ(), "GIT_INDEX_FILE="+index)
	run := func(stdin string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, out, e)
		}
		return strings.TrimSpace(string(out))
	}
	for path, body := range files {
		hash := run(body, "hash-object", "-w", "--stdin")
		run("100644 "+hash+"\t"+path+"\n", "update-index", "--add", "--index-info")
	}
	tree := run("", "write-tree")
	args := []string{}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return run("tree "+tree+"\n\ncrafted\n", append([]string{"commit-tree", tree}, args...)...)
}

// The guard must prove the content, not merely the parent set: a commit with the
// reviewed parents but unreviewed files may never become the pushed main.
func TestMainSHAAllowedRejectsUnreviewedTrees(t *testing.T) {
	m, task, x, source, _ := pushFixture(t)
	review := pushReview(t, m, task)
	freeze := workbench.FreezeFromReview(review)
	main0 := workspaceGit(t, source, "rev-parse", "refs/heads/"+review.MainBranch)
	wsHead := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	if wsHead == "" || main0 == "" {
		t.Fatal("fixture")
	}
	cases := []struct {
		name  string
		sha   string
		allow bool
	}{
		{"reviewed local main", main0, true},
		{"reviewed task commit", wsHead, true},
		{"root commit with unreviewed file", craftCommit(t, source, nil, map[string]string{"private.txt": "sekrit\n"}), false},
		{"single parent with unreviewed file", craftCommit(t, source, []string{main0}, map[string]string{"private.txt": "sekrit\n"}), false},
		{"both reviewed parents with unreviewed file", craftCommit(t, source, []string{main0, wsHead}, map[string]string{
			"base": "base\n", "feature.txt": "feature\n", "private.txt": "sekrit\n",
		}), false},
	}
	for _, c := range cases {
		if got := mainSHAAllowed(m.ctx, source, freeze, wsHead, c.sha); got != c.allow {
			t.Errorf("%s: mainSHAAllowed=%v want %v (%s)", c.name, got, c.allow, c.sha[:8])
		}
	}
}

// An ordinary merge build of exactly the reviewed parents is still allowed: the
// content proof must not block the normal non-fast-forward integration.
func TestMainSHAAllowedAcceptsExactMergeResult(t *testing.T) {
	m, task, x, source, _ := pushFixture(t)
	review := pushReview(t, m, task)
	freeze := workbench.FreezeFromReview(review)
	main0 := workspaceGit(t, source, "rev-parse", "refs/heads/"+review.MainBranch)
	wsHead := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	tree, e := exec.Command("git", "-C", source, "merge-tree", "--write-tree", main0, wsHead).Output()
	if e != nil {
		t.Skip("git merge-tree unavailable")
	}
	// A genuine merge commit: the real merge tree with exactly the two reviewed
	// parents, which is what an ordinary non-fast-forward integration produces.
	merged, e := exec.Command("git", "-C", source, "commit-tree", strings.TrimSpace(string(tree)),
		"-p", main0, "-p", wsHead, "-m", "Merge task").Output()
	if e != nil {
		t.Fatalf("commit-tree: %v", e)
	}
	mergedSHA := strings.TrimSpace(string(merged))
	if !mainSHAAllowed(m.ctx, source, freeze, wsHead, mergedSHA) {
		t.Fatalf("exact merge result of the reviewed parents must be allowed")
	}
}

// PushOnly must not re-run a local commit or merge: the already-merged push may
// not move ORIG_HEAD or change the commit count of the task workspace.
func TestPushOnlyDoesNotReMergeLocally(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	local, e := m.PreviewMergeReview(t.Context(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !local.CanMerge {
		t.Fatal(local, e)
	}
	if op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "local", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: local.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"}); e != nil {
		t.Fatal(e)
	} else if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatalf("local merge: %#v", op)
	}
	review := pushReview(t, m, task)
	if !review.PushOnly {
		t.Fatalf("expected push-only: %#v", review)
	}
	mainCount := func() string { return workspaceGit(t, source, "rev-list", "--count", "HEAD") }
	mainBefore := workspaceGit(t, source, "rev-parse", "refs/heads/"+review.MainBranch)
	countBefore := mainCount()
	origBefore := workspaceGit(t, source, "rev-parse", "--verify", "ORIG_HEAD")
	taskCount := workspaceGit(t, x.Workspace, "rev-list", "--count", "HEAD")
	taskHead := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	op := startPush(t, m, task, review, "push-only-pure")
	if op.Phase != "succeeded" || conn.count() != 1 {
		t.Fatalf("op %#v pushes %d", op, conn.count())
	}
	if got := workspaceGit(t, source, "rev-parse", "refs/heads/"+review.MainBranch); got != mainBefore {
		t.Fatalf("main moved: %s -> %s", mainBefore, got)
	}
	if got := mainCount(); got != countBefore {
		t.Fatalf("main commit count changed: %s -> %s", countBefore, got)
	}
	if got := workspaceGit(t, source, "rev-parse", "--verify", "ORIG_HEAD"); got != origBefore {
		t.Fatalf("push-only re-ran a merge: ORIG_HEAD %q -> %q", origBefore, got)
	}
	if got, head := workspaceGit(t, x.Workspace, "rev-list", "--count", "HEAD"), workspaceGit(t, x.Workspace, "rev-parse", "HEAD"); got != taskCount || head != taskHead {
		t.Fatalf("task workspace rewritten: %s/%s -> %s/%s", taskCount, taskHead, got, head)
	}
	if op.MainAfterSHA != mainBefore || op.IntendedSHA != mainBefore {
		t.Fatalf("facts: %#v", op)
	}
	if bareRef(t, conn.bare, review.MainRef) != mainBefore {
		t.Fatalf("remote did not receive the reviewed main")
	}
}

// When main already contains the reviewed content through further commits, the
// push still uploads the existing main without generating another merge.
// A main that advanced after an ordinary local merge is re-reviewed (not re-merged)
// and pushed as-is: push-only must neither re-run the merge nor refuse the newer
// main, while an advanced main carrying unreviewed content stays refused by the
// content proof in TestMainSHAAllowedRejectsUnreviewedTrees.
func TestPushOnlyReusesAdvancedMainWithoutMerging(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	local, e := m.PreviewMergeReview(t.Context(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil || !local.CanMerge {
		t.Fatal(local, e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "local", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: local.ID, CommitMessage: "feature", Mode: workbench.MergeModeLocal, RoundID: "round-1"})
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatalf("local merge: %#v", op)
	}
	// A later ordinary commit on main; the reviewed task content is retained.
	if e := os.WriteFile(filepath.Join(source, "after.txt"), []byte("after\n"), 0600); e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, source, "add", "after.txt")
	workspaceGit(t, source, "commit", "-q", "-m", "after")
	advanced := workspaceGit(t, source, "rev-parse", "HEAD")
	review := pushReview(t, m, task)
	freeze := workbench.FreezeFromReview(review)
	if !freeze.PushOnly || freeze.LocalMainSHA != advanced {
		t.Fatalf("review did not freeze the advanced main: %#v", freeze)
	}
	headBefore := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	countBefore := workspaceGit(t, source, "rev-list", "--count", "HEAD")
	origBefore := workspaceGit(t, source, "rev-parse", "--verify", "ORIG_HEAD")
	if op := startPush(t, m, task, review, "push-only-advanced"); op.Phase != "succeeded" || op.MainAfterSHA != advanced {
		t.Fatalf("push-only: %#v", op)
	}
	if got := bareRef(t, conn.bare, review.MainRef); got != advanced {
		t.Fatalf("remote %q != advanced main %q", got, advanced)
	}
	if workspaceGit(t, source, "rev-list", "--count", "HEAD") != countBefore ||
		workspaceGit(t, source, "rev-parse", "--verify", "ORIG_HEAD") != origBefore ||
		workspaceGit(t, x.Workspace, "rev-parse", "HEAD") != headBefore {
		t.Fatal("push-only re-ran a local commit or merge")
	}
}
