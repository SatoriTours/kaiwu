package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// reviewLoop is reviewRemote with the execution identity the safety proof needs.
func reviewLoop(t *testing.T, f reviewProject, transport RemoteTransport, ok bool, round string) (workbench.MergeReview, error) {
	t.Helper()
	runner := NewRemoteRunner(RemoteConfig{Transport: transport, Resolver: reviewCredentials(t, ok)})
	return ReviewRemote(context.Background(), runner, f.project, f.task, f.x, reviewLocal(t, f), workbench.MergeReviewRequest{ProjectID: "p", TaskID: f.task.ID, Mode: workbench.MergeModePush, RoundID: round})
}

// mainExtra appends one commit to the project main branch, as a user would while
// the task is in flight.
func mainExtra(t *testing.T, f reviewProject, name, body string) string {
	t.Helper()
	if e := os.WriteFile(filepath.Join(f.project.Path, name), []byte(body), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, f.project.Path, "add", name)
	g(t, f.project.Path, "commit", "-q", "-m", "main "+name)
	sha, _ := runGit(t, f.project.Path, "rev-parse", "HEAD")
	return strings.TrimSpace(sha)
}

func subjects(uploads []workbench.MergeUpload) []string {
	out := make([]string, 0, len(uploads))
	for _, u := range uploads {
		out = append(out, u.Subject)
	}
	return out
}

// The upload set is every commit reachable from both merge inputs and not from
// the observed remote: a new main commit must be disclosed, not just the task.
func TestReviewRemoteUploadsBothInputs(t *testing.T) {
	f := reviewFixture(t)
	extra := mainExtra(t, f, "later.txt", "later\n")
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if !review.CanPush || review.LocalMainSHA != extra {
		t.Fatalf("push not allowed: %#v", review)
	}
	got := strings.Join(subjects(review.Uploads), ",")
	if !strings.Contains(got, "main later.txt") || !strings.Contains(got, "add second") || !strings.Contains(got, "add feature") {
		t.Fatalf("upload range incomplete: %q", got)
	}
	// The already observed commit must not be uploaded again.
	for _, u := range review.Uploads {
		if u.SHA == f.mainSHA {
			t.Fatal("observed commit in upload set")
		}
	}
}

// AlreadyMerged now means "local main contains the task", so it stays push-only
// even when the remote branch does not exist yet.
func TestReviewRemoteLocalAlreadyMergedIsPushOnly(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	merged, _ := runGit(t, f.project.Path, "rev-parse", "HEAD")
	merged = strings.TrimSpace(merged)

	for _, c := range []struct {
		name string
		bare string
	}{
		{"remote behind", f.bare},
		{"remote missing", bareEmpty(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			review, e := reviewLoop(t, f, localTransport(t, c.bare), true, "round-1")
			if e != nil {
				t.Fatal(e)
			}
			if !review.AlreadyMerged || !review.PushOnly || !review.CanPush {
				t.Fatalf("local merge not push-only: %#v", review)
			}
			if review.LocalMainSHA != merged || review.SafetyProof == "" || review.ReviewFingerprint == "" {
				t.Fatalf("already merged proof incomplete: %#v", review)
			}
			if len(review.Uploads) == 0 {
				t.Fatal("upload set empty although remote lacks the merge")
			}
		})
	}
}

// A remote that already contains the task does not make the local extra main
// commits unnecessary: they still have to be shown as uploads.
func TestReviewRemoteRemoteHasTaskButMainMoved(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.x.Workspace, "push", "--quiet", f.bare, f.taskSHA+":refs/heads/main")
	extra := mainExtra(t, f, "later.txt", "later\n")
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	// The remote holding the task does not mean the local main has it, and the
	// extra local main commit still has to be uploaded.
	if !review.CanPush || review.AlreadyMerged || review.PushOnly {
		t.Fatalf("state wrong: %#v", review)
	}
	if review.LocalMainSHA != extra || len(review.Uploads) != 1 || review.Uploads[0].Subject != "main later.txt" {
		t.Fatalf("main extra commit missing from uploads: %#v %#v", review.Uploads, review)
	}
}

// Only when the remote already holds main and task and nothing is pending is
// there nothing to push.
func TestReviewRemoteRemoteAlreadyHasEverything(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	g(t, f.project.Path, "push", "--quiet", f.bare, "refs/heads/main:refs/heads/main")
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || len(review.Uploads) != 0 || !strings.Contains(review.PushReason, "无需推送") {
		t.Fatalf("nothing-to-push not reported: %#v", review)
	}
	if !review.AlreadyMerged || !review.PushOnly {
		t.Fatalf("local merge state lost: %#v", review)
	}
}

// A diverged remote cannot be fast-forwarded by the planned result.
func TestReviewRemoteDivergedRemoteBlocks(t *testing.T) {
	f := reviewFixture(t)
	// A remote-only commit that the local repository does know about: the two
	// planned inputs cannot fast-forward it, so forcing would be required.
	tree, _ := runGit(t, f.project.Path, "rev-parse", f.mainSHA+"^{tree}")
	commit, e := runGit(t, f.project.Path, "commit-tree", strings.TrimSpace(tree), "-p", f.mainSHA, "-m", "diverged")
	if e != nil {
		t.Fatal(e)
	}
	diverged := strings.TrimSpace(commit)
	g(t, f.project.Path, "update-ref", "refs/heads/diverged", diverged)
	bare := bareEmpty(t)
	if out, e := runGit(t, f.project.Path, "push", "--quiet", bare, "refs/heads/diverged:refs/heads/main"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	review, e := reviewLoop(t, f, localTransport(t, bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || !strings.Contains(review.PushReason, "快进") {
		t.Fatalf("diverged remote not blocked: %#v", review)
	}
}

// Local safety refusals must not be erased into canPush=true.
func TestReviewRemoteLocalRejectionBlocksPush(t *testing.T) {
	f := reviewFixture(t)
	if e := os.WriteFile(filepath.Join(f.x.Workspace, ".env"), []byte("SECRET=1"), 0o600); e != nil {
		t.Fatal(e)
	}
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || review.PushReason == "" {
		t.Fatalf("private pending file not blocked: %#v", review)
	}
	if review.CanMerge {
		t.Fatal("local preview should have refused the private file")
	}
}

// A private file added and then deleted inside the history that would be
// uploaded is still refused.
func TestReviewRemotePrivateHistoryInMainBlocks(t *testing.T) {
	f := reviewFixture(t)
	if e := os.WriteFile(filepath.Join(f.project.Path, ".env"), []byte("SECRET=1"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, f.project.Path, "add", "-f", ".env")
	g(t, f.project.Path, "commit", "-q", "-m", "add secret")
	g(t, f.project.Path, "rm", "-q", ".env")
	g(t, f.project.Path, "commit", "-q", "-m", "drop secret")
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || !strings.Contains(review.PushReason, "私密") {
		t.Fatalf("private history not blocked: %#v", review)
	}
}

// The fingerprint covers the main content, the round and the remote config even
// for an already merged task.
func TestReviewRemoteFingerprintBindsEverything(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	base, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if base.ReviewFingerprint == "" || base.SafetyProof == "" {
		t.Fatalf("already merged fingerprint missing: %#v", base)
	}
	other, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-2")
	if e != nil || other.ReviewFingerprint == base.ReviewFingerprint {
		t.Fatal("round not bound")
	}
	mainExtra(t, f, "later.txt", "later\n")
	moved, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil || moved.ReviewFingerprint == base.ReviewFingerprint {
		t.Fatal("extra main commit not bound")
	}
	g(t, f.project.Path, "remote", "set-url", "origin", "https://github.com/other/repo.git")
	reconfigured, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil || reconfigured.ReviewFingerprint == moved.ReviewFingerprint {
		t.Fatal("remote configuration not bound")
	}
}

// A broken worktree ownership record blocks the push even when the local
// preview was produced before the record changed.
func TestReviewRemoteBrokenIdentityBlocksPush(t *testing.T) {
	f := reviewFixture(t)
	local := reviewLocal(t, f)
	owner := filepath.Join(filepath.Dir(f.x.Workspace), "owner.json")
	if e := os.WriteFile(owner, []byte(`{"TaskID":"other","Ready":true}`), 0o600); e != nil {
		t.Fatal(e)
	}
	runner := NewRemoteRunner(RemoteConfig{Transport: localTransport(t, f.bare), Resolver: reviewCredentials(t, true)})
	review, e := ReviewRemote(context.Background(), runner, f.project, f.task, f.x, local, workbench.MergeReviewRequest{ProjectID: "p", TaskID: f.task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || review.SafetyProof != "" || !strings.Contains(review.PushReason, "归属") {
		t.Fatalf("broken identity not blocked: %#v", review)
	}
	if !review.CanMerge || review.LocalMainSHA == "" {
		t.Fatalf("local proof lost: %#v", review)
	}
}

// An already merged local main still has to carry a full safety proof and a
// non-empty fingerprint bound to its real content.
func TestReviewRemoteAlreadyMergedFingerprintUsesContent(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if !review.AlreadyMerged || review.Fingerprint == "" || review.ReviewFingerprint == "" || review.SafetyProof == "" {
		t.Fatalf("already merged proof incomplete: %#v", review)
	}
	mainExtra(t, f, "later.txt", "later\n")
	moved, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil || moved.ReviewFingerprint == review.ReviewFingerprint {
		t.Fatal("extra main commit not bound into already merged review")
	}
}
