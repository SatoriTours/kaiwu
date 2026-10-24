package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// reviewBare mirrors the project's main branch into a temporary bare repository
// standing in for the remote. The production URL validation stays untouched; the
// narrow transport injection below only redirects the frozen legal URL.
func reviewBare(t *testing.T, source string, refs ...string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	if out, e := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); e != nil {
		t.Fatalf("bare init: %s %v", out, e)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "refs/") {
			b, e := exec.Command("git", "-C", source, "rev-parse", "--abbrev-ref", "HEAD").Output()
			if e != nil {
				t.Fatal(e)
			}
			ref = "refs/heads/" + strings.TrimSpace(string(b))
		}
		if out, e := exec.Command("git", "-C", source, "push", "--quiet", bare, ref+":"+ref).CombinedOutput(); e != nil {
			t.Fatalf("bare push %s: %s %v", ref, out, e)
		}
	}
	return bare
}

func reviewTransport(bare string) gitops.RemoteTransport {
	return gitops.RemoteTransportFunc(func(_ context.Context, _ string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
		// This transport runs no command at all, so it can prove it spawned none.
		if f, ok := lease.(interface{ NoProcess() }); ok {
			f.NoProcess()
		}
		if !strings.HasPrefix(target.URL, "https://github.com/") {
			return "", errors.New("unexpected url")
		}
		var out strings.Builder
		for i, a := range args {
			if a == "ls-remote" {
				for _, ref := range args[i+1:] {
					if !strings.HasPrefix(ref, "refs/") {
						continue
					}
					b, e := exec.Command("git", "-C", bare, "rev-parse", "--verify", ref).Output()
					if e != nil {
						return "", errors.New("exit status 2")
					}
					out.WriteString(strings.TrimSpace(string(b)) + "\t" + ref + "\n")
				}
				return out.String(), nil
			}
		}
		return "", errors.New("unexpected command")
	})
}

func reviewManager(t *testing.T) (*Manager, workbench.Task, workbench.Execution, string) {
	t.Helper()
	m, task, x, source := integrationFixture(t)
	if out, e := exec.Command("git", "-C", source, "remote", "add", "origin", "https://github.com/owner/repo.git").CombinedOutput(); e != nil {
		t.Fatalf("remote add: %s %v", out, e)
	}
	if e := os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("feature"), 0600); e != nil {
		t.Fatal(e)
	}
	// Commit so the review has a real task commit to upload; an uncommitted
	// checkout is covered separately as the pending-commit case.
	workspaceGit(t, x.Workspace, "add", "feature.txt")
	workspaceGit(t, x.Workspace, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "feature")
	finishedReview(t, m, task)
	return m, task, x, source
}

// The push review observes the remote and lists the full upload set without
// touching the checkout, the index or the refs.
func TestPreviewPushReviewListsUploadsReadOnly(t *testing.T) {
	m, task, x, source := reviewManager(t)
	bare := reviewBare(t, source, "HEAD")
	resolver, e := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) { return "token", true }})
	if e != nil {
		t.Fatal(e)
	}
	m.cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver, Transport: reviewTransport(bare)})
	head := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	stBefore := m.store.Snapshot()

	review, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil || !review.CanPush || review.PushReason != "" {
		t.Fatal(review, e)
	}
	if review.Mode != workbench.MergeModePush || review.RoundID != "round-1" || review.RemoteName != "origin" {
		t.Fatalf("review binding: %#v", review)
	}
	if review.RemoteURL != "https://github.com/owner/repo.git" || review.MainRef != "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch || review.ObservedRemoteSHA == "" {
		t.Fatalf("remote proof: %#v", review)
	}
	if len(review.Uploads) != 1 || review.Uploads[0].SHA != strings.TrimSpace(head) || review.Uploads[0].Subject != "feature" {
		t.Fatalf("upload set: %#v (head %s)", review.Uploads, head)
	}
	if review.ReviewFingerprint == "" || review.ReviewFingerprint == review.Fingerprint {
		t.Fatal("missing review fingerprint")
	}
	// Read-only: the preview must not acknowledge, merge, or rewrite Git state.
	if after := m.store.Snapshot(); after.Revision != stBefore.Revision || after.Tasks[task.ID].Status != "needs_confirmation" {
		t.Fatalf("review wrote state: %d -> %d", stBefore.Revision, after.Revision)
	}
	if got := workspaceGit(t, x.Workspace, "rev-parse", "HEAD"); got != head {
		t.Fatalf("worktree head changed: %s -> %s", head, got)
	}
	if got := workspaceGit(t, x.Workspace, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree touched: %s", got)
	}
	if got := workspaceGit(t, source, "rev-parse", "refs/heads/"+m.store.Snapshot().Projects[task.ProjectID].DefaultBranch); strings.TrimSpace(got) == strings.TrimSpace(head) {
		t.Fatal("main branch advanced")
	}
}

// An unconfirmed push review cannot be queued: the round is not acknowledged.
func TestPushReviewRequiresFinishedRound(t *testing.T) {
	m, task, _, source := reviewManager(t)
	bare := reviewBare(t, source, "HEAD")
	resolver, _ := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) { return "token", true }})
	m.cfg.Remote = gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver, Transport: reviewTransport(bare)})
	review, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil || !review.CanPush {
		t.Fatal(review, e)
	}
	// A mismatched round never inherits the accepted review.
	if _, _, e = m.StartMerge(workbench.MergeRequest{OperationID: "push-1", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-9"}); e == nil {
		t.Fatal("round mismatch accepted")
	}
	if st := m.store.Snapshot(); len(st.Merges) != 0 || st.Tasks[task.ID].Status != "needs_confirmation" {
		t.Fatal("refused push still wrote state")
	}
}

// Without a remote runner the local proof survives and only push is blocked.
func TestPushReviewWithoutRunnerBlocksOnlyPush(t *testing.T) {
	m, task, _, _ := reviewManager(t)
	review, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil || review.CanPush || !strings.Contains(review.PushReason, "远程") {
		t.Fatal(review, e)
	}
	if review.ID == "" || review.TaskSHA == "" {
		t.Fatal("local proof lost")
	}
	if _, e := m.PreviewMergeReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"}); e != nil {
		t.Fatal("local review broken without remote:", e)
	}
}

// A local-mode request must not be routed through the push review.
func TestPushReviewRejectsLocalMode(t *testing.T) {
	m, task, _, _ := reviewManager(t)
	if _, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModeLocal, RoundID: "round-1"}); e == nil {
		t.Fatal("local mode accepted as push")
	}
}
