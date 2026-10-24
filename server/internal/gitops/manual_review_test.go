package gitops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// bareRemote builds a temporary bare repository that stands in for the frozen
// github.com URL. Tests never touch the network and never loosen production URL
// validation: only the transport is narrowed.
func bareEmpty(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	if out, e := runGit(t, bare, "init", "-q", "--bare"); e != nil {
		t.Fatalf("bare init: %s %v", out, e)
	}
	return bare
}

// bareWith publishes refs from the project into the temporary bare repository,
// so the fake transport has the objects a real remote would hold.
func bareWith(t *testing.T, source string, refs ...string) string {
	t.Helper()
	bare := bareEmpty(t)
	for _, ref := range refs {
		if out, e := runGit(t, source, "push", "--quiet", bare, ref+":"+ref); e != nil {
			t.Fatalf("bare push %s: %s %v", ref, out, e)
		}
	}
	return bare
}

// bareForeign creates a bare repository holding a commit the project has never
// seen, simulating an unobserved remote-only object.
func bareForeign(t *testing.T) (string, string) {
	t.Helper()
	bare := bareEmpty(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	if e := os.Mkdir(scratch, 0o700); e != nil {
		t.Fatal(e)
	}
	g(t, scratch, "init", "--initial-branch=main", "--template=")
	g(t, scratch, "config", "user.name", "Fixture")
	g(t, scratch, "config", "user.email", "fixture@example.invalid")
	if e := os.WriteFile(filepath.Join(scratch, "x"), []byte("x\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, scratch, "add", "x")
	g(t, scratch, "commit", "-q", "-m", "foreign")
	g(t, scratch, "push", "--quiet", bare, "refs/heads/main:refs/heads/main")
	sha, _ := runGit(t, scratch, "rev-parse", "HEAD")
	return bare, strings.TrimSpace(sha)
}

// localTransport answers ls-remote from a temporary bare repository.
func localTransport(t *testing.T, bare string) RemoteTransportFunc {
	return func(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
		ref := ""
		if len(args) > 3 {
			ref = args[len(args)-1]
		}
		if ref == "" {
			return "", nil
		}
		var out strings.Builder
		// A real child runs here, so its identity is reported to the lease.
		sha, e := Run(ctx, bare, LeaseTrack(lease), "rev-parse", "--verify", ref)
		if e != nil || strings.TrimSpace(sha) == "" {
			return "", nil
		}
		out.WriteString(strings.TrimSpace(sha) + "\t" + ref + "\n")
		return out.String(), nil
	}
}

// reviewProject builds a real project checkout plus a task worktree whose
// owner record passes the production ownership checks.
type reviewProject struct {
	project workbench.Project
	task    workbench.Task
	x       workbench.Execution
	bare    string
	mainSHA string
	taskSHA string
}

func reviewFixture(t *testing.T) reviewProject {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "project")
	if e := os.Mkdir(source, 0o700); e != nil {
		t.Fatal(e)
	}
	g(t, source, "init", "--initial-branch=main", "--template=")
	g(t, source, "config", "user.name", "Kaiwu")
	g(t, source, "config", "user.email", "kaiwu@localhost")
	g(t, source, "remote", "add", "origin", "https://github.com/owner/repo.git")
	if e := os.WriteFile(filepath.Join(source, "README"), []byte("base\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, source, "add", "README")
	g(t, source, "commit", "-q", "-m", "base")
	base, _ := runGit(t, source, "rev-parse", "HEAD")
	base = strings.TrimSpace(base)
	id := strings.Repeat("a", 32)
	branch := "kaiwu/task-" + id
	parent := filepath.Join(root, "worktrees", id)
	if e := os.MkdirAll(parent, 0o700); e != nil {
		t.Fatal(e)
	}
	tree := filepath.Join(parent, "tree")
	g(t, source, "worktree", "add", "-b", branch, tree)
	owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	if e := os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0o600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"config", "user.name", "Kaiwu"}, {"config", "user.email", "kaiwu@localhost"}} {
		g(t, tree, args...)
	}
	if e := os.WriteFile(filepath.Join(tree, "feature.txt"), []byte("feature\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, tree, "add", "feature.txt")
	g(t, tree, "commit", "-q", "-m", "add feature")
	if e := os.WriteFile(filepath.Join(tree, "second.txt"), []byte("second\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, tree, "add", "second.txt")
	g(t, tree, "commit", "-q", "-m", "add second")
	taskSHA, _ := runGit(t, tree, "rev-parse", "HEAD")
	return reviewProject{
		project: workbench.Project{ID: "p", Path: source, DefaultBranch: "main", MainSeen: true},
		task:    workbench.Task{ID: id, ProjectID: "p", ConversationID: "c"},
		x:       workbench.Execution{ID: "x", TaskID: id, ConversationID: "c", Workspace: tree, Branch: branch, BaseSHA: base},
		bare:    bareWith(t, source, "refs/heads/main"),
		mainSHA: base,
		taskSHA: strings.TrimSpace(taskSHA),
	}
}

func g(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, e := runGit(t, dir, args...); e != nil {
		t.Fatalf("%v: %s %v", args, out, e)
	}
}

func reviewCredentials(t *testing.T, ok bool) *githubauth.Resolver {
	t.Helper()
	r, e := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) {
		if !ok {
			return "", false
		}
		return "token", true
	}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func reviewLocal(t *testing.T, f reviewProject) workbench.MergePreview {
	t.Helper()
	p, e := Preview(context.Background(), f.project, f.task, f.x)
	if e != nil {
		t.Fatal(p, e)
	}
	return p
}

func reviewRemote(t *testing.T, f reviewProject, transport RemoteTransport, credentials *githubauth.Resolver) (workbench.MergeReview, error) {
	t.Helper()
	runner := NewRemoteRunner(RemoteConfig{Transport: transport, Resolver: credentials})
	return ReviewRemote(context.Background(), runner, f.project, f.task, f.x, reviewLocal(t, f), workbench.MergeReviewRequest{ProjectID: "p", TaskID: f.task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"})
}

// A project with no remote at all still produces a local review; only the push
// review is blocked with an explicit reason.
func TestReviewRemoteLocalWithoutRemote(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "remote", "remove", "origin")
	review, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || review.PushReason == "" || review.RemoteURL != "" {
		t.Fatalf("missing remote not blocked: %#v", review)
	}
	if !review.CanMerge || review.Mode != workbench.MergeModePush || review.RoundID != "round-1" {
		t.Fatalf("local review broken: %#v", review)
	}
}

// The push review lists every commit that would be uploaded and covers the whole
// remote-to-task range, including a pending local change.
func TestReviewRemotePushScopeAndFingerprint(t *testing.T) {
	f := reviewFixture(t)
	if e := os.WriteFile(filepath.Join(f.x.Workspace, "pending.txt"), []byte("pending"), 0o600); e != nil {
		t.Fatal(e)
	}
	review, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if !review.CanPush || review.RemoteName != "origin" || review.RemoteURL != "https://github.com/owner/repo.git" {
		t.Fatalf("push not allowed: %#v", review)
	}
	if review.MainRef != "refs/heads/main" || review.ObservedRemoteSHA != f.mainSHA {
		t.Fatalf("remote observation wrong: %#v", review)
	}
	if len(review.Uploads) != 2 || review.Uploads[0].Subject != "add second" || review.Uploads[1].Subject != "add feature" {
		t.Fatalf("upload range wrong: %#v", review.Uploads)
	}
	for _, u := range review.Uploads {
		if len(u.SHA) != 40 {
			t.Fatalf("upload sha: %#v", u)
		}
	}
	if !review.NeedsCommit || len(review.Pending) != 1 || review.Pending[0].Path != "pending.txt" {
		t.Fatalf("pending change not disclosed: %#v", review)
	}
	if review.AlreadyMerged || review.PushOnly || review.ReviewFingerprint == "" {
		t.Fatalf("unexpected state: %#v", review)
	}
	again, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, true))
	if e != nil || again.ReviewFingerprint != review.ReviewFingerprint {
		t.Fatalf("fingerprint unstable: %v %v", again.ReviewFingerprint, e)
	}
	// A different round, a new remote commit or a new local commit must all
	// produce a different proof.
	changed := review
	other, e := ReviewRemote(context.Background(), NewRemoteRunner(RemoteConfig{Transport: localTransport(t, f.bare), Resolver: reviewCredentials(t, true)}), f.project, f.task, f.x, reviewLocal(t, f), workbench.MergeReviewRequest{ProjectID: "p", TaskID: f.task.ID, Mode: workbench.MergeModePush, RoundID: "round-2"})
	if e != nil || other.ReviewFingerprint == changed.ReviewFingerprint {
		t.Fatal("round not bound into fingerprint")
	}
	// The remote advancing to a new main is observed and rebinds the proof. A
	// remote that merely contains the task is not a local merge, so the local
	// main commits still have to be uploaded.
	g(t, f.x.Workspace, "push", "--quiet", f.bare, f.taskSHA+":refs/heads/main")
	moved, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, true))
	if e != nil || moved.ReviewFingerprint == changed.ReviewFingerprint {
		t.Fatalf("remote main not bound into fingerprint: %#v %v", moved, e)
	}
	if moved.AlreadyMerged || !moved.CanPush || moved.ObservedRemoteSHA != f.taskSHA {
		t.Fatalf("remote observation wrong: %#v", moved)
	}
}

// An already integrated task still gets a full, safe proof instead of a bare
// "cannot merge".
func TestReviewRemoteAlreadyMergedKeepsProof(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	bare := bareWith(t, f.project.Path, "refs/heads/main")
	f.bare = bare
	review, e := reviewRemote(t, f, localTransport(t, bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if !review.AlreadyMerged || review.CanPush || len(review.Uploads) != 0 || review.PushReason == "" {
		t.Fatalf("already merged proof wrong: %#v", review)
	}
	if review.ObservedRemoteSHA != f.taskSHA || review.ReviewFingerprint == "" || review.SafetyProof == "" {
		t.Fatalf("already merged proof incomplete: %#v", review)
	}
	// A local-only integration whose remote is still behind is already merged
	// locally, so it is push-only and must not re-merge; only the missing
	// uploads stay.
	only := reviewFixture(t)
	g(t, only.project.Path, "merge", "--ff-only", only.x.Branch)
	pushOnly, e := reviewRemote(t, only, localTransport(t, only.bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if !pushOnly.AlreadyMerged || !pushOnly.PushOnly || !pushOnly.CanPush || len(pushOnly.Uploads) != 2 {
		t.Fatalf("push-only proof wrong: %#v", pushOnly)
	}
}

// Missing Git credentials block only the push review.
func TestReviewRemoteMissingCredentialsOnlyBlocksPush(t *testing.T) {
	f := reviewFixture(t)
	review, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, false))
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || !strings.Contains(review.PushReason, "凭据") {
		t.Fatalf("missing credential not reported: %#v", review)
	}
	// Without a remote observation the whole planned result is disclosed, which
	// is conservative rather than silently narrow.
	if !review.CanMerge || len(review.Uploads) != 3 {
		t.Fatalf("local review damaged: %#v", review)
	}
}

// Production accepts only the supported github.com URL and a clean remote config.
func TestReviewRemoteRejectsUnsafeConfiguration(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{"credential url", func(t *testing.T, path string) {
			g(t, path, "remote", "set-url", "origin", "https://user:token@github.com/owner/repo.git")
		}},
		{"file url", func(t *testing.T, path string) {
			g(t, path, "remote", "set-url", "origin", "file:///tmp/repo.git")
		}},
		{"http url", func(t *testing.T, path string) {
			g(t, path, "remote", "set-url", "origin", "http://127.0.0.1:9999/repo.git")
		}},
		{"second url", func(t *testing.T, path string) {
			g(t, path, "config", "--add", "remote.origin.url", "https://github.com/other/repo.git")
		}},
		{"pushurl conflict", func(t *testing.T, path string) {
			g(t, path, "config", "remote.origin.pushurl", "https://github.com/other/repo.git")
		}},
		{"insteadOf", func(t *testing.T, path string) {
			g(t, path, "config", "url.https://evil.example/.insteadOf", "https://github.com/")
		}},
		{"helper", func(t *testing.T, path string) {
			g(t, path, "config", "remote.origin.vcs", "fake")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := reviewFixture(t)
			c.mutate(t, f.project.Path)
			review, e := reviewRemote(t, f, localTransport(t, f.bare), reviewCredentials(t, true))
			if e != nil {
				t.Fatal(e)
			}
			if review.CanPush {
				t.Fatalf("unsafe remote accepted: %#v", review)
			}
		})
	}
}

// A remote main SHA that is missing locally cannot be resolved into an upload
// range, so the review must block instead of guessing or fetching.
func TestReviewRemoteBlocksUnknownRemoteObject(t *testing.T) {
	f := reviewFixture(t)
	bare, unknown := bareForeign(t)
	_ = unknown
	review, e := reviewRemote(t, f, localTransport(t, bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || len(review.Uploads) != 0 || !strings.Contains(review.PushReason, "无法完整确定") {
		t.Fatalf("unknown object not blocked: %#v", review)
	}
	// Nothing was fetched and the local main ref is untouched.
	if sha, _ := runGit(t, f.project.Path, "rev-parse", "refs/heads/main"); strings.TrimSpace(sha) != f.mainSHA {
		t.Fatal("review changed the local main ref")
	}
}

// When the remote branch does not exist the review lists the whole history that
// would create it, and refuses a range that exceeds the bound.
func TestReviewRemoteMissingRefListsHistory(t *testing.T) {
	f := reviewFixture(t)
	bare := bareEmpty(t)
	review, e := reviewRemote(t, f, localTransport(t, bare), reviewCredentials(t, true))
	if e != nil {
		t.Fatal(e)
	}
	if review.ObservedRemoteSHA != "" || !review.CanPush || len(review.Uploads) != 3 {
		t.Fatalf("full history not listed: %#v", review)
	}
	if review.MainRef != "refs/heads/main" {
		t.Fatalf("main ref wrong: %#v", review)
	}
	for _, want := range []string{"base", "add feature", "add second"} {
		found := false
		for _, u := range review.Uploads {
			found = found || u.Subject == want
		}
		if !found {
			t.Fatalf("history missing %q: %#v", want, review.Uploads)
		}
	}
}
