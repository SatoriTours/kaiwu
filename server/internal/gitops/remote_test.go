package gitops

import (
	"context"
	"errors"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// TestDeliveryRemoteRefScope refuses credential URLs, multiple push URLs,
// URL rewrites, remote helpers, non-GitHub hosts and non-branch refs.
func TestDeliveryRemoteRefScope(t *testing.T) {
	root := t.TempDir()
	project := initGitRepo(t, root)

	policy := func(repoURL string) workbench.ProjectDeliveryPolicy {
		return workbench.ProjectDeliveryPolicy{
			Repository: workbench.DeliveryRepository{ID: 123, Owner: "owner", Name: "repo", URL: repoURL, MainBranch: "main"},
		}
	}
	project2 := workbench.Project{ID: "p1", Path: project}
	resolver, e := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) { return "", false }})
	if e != nil {
		t.Fatal(e)
	}

	t.Run("frozen https url resolves to task and main refs", func(t *testing.T) {
		target, e := ResolveRemote(context.Background(), project2, policy("https://github.com/owner/repo.git"), "0123456789abcdef0123456789abcdef")
		if e != nil {
			t.Fatalf("ResolveRemote: %v", e)
		}
		if target.URL != "https://github.com/owner/repo.git" || target.MainRef != "main" || target.TaskRef != "refs/heads/kaiwu/task-0123456789abcdef0123456789abcdef" {
			t.Fatalf("target: %+v", target)
		}
		if target.RepositoryID != 123 {
			t.Fatalf("repository id: %+v", target)
		}
	})
	_ = resolver

	t.Run("credential urls are refused", func(t *testing.T) {
		for _, bad := range []string{
			"https://user:pass@github.com/owner/repo.git",
			"https://x-access-token:tok@github.com/owner/repo.git",
		} {
			if _, e := ResolveRemote(context.Background(), project2, policy(bad), "0123456789abcdef0123456789abcdef"); e == nil {
				t.Fatalf("credential url %q accepted", bad)
			}
		}
	})

	t.Run("non github hosts and protocols are refused", func(t *testing.T) {
		for _, bad := range []string{
			"https://evil.invalid/owner/repo.git",
			"git://github.com/owner/repo.git",
			"ssh://git@evil.invalid/owner/repo.git",
		} {
			if _, e := ResolveRemote(context.Background(), project2, policy(bad), "0123456789abcdef0123456789abcdef"); e == nil {
				t.Fatalf("url %q accepted", bad)
			}
		}
	})

	t.Run("multiple push urls are refused", func(t *testing.T) {
		if out, e := gitCfg(t, project, "remote.origin.pushurl", "https://github.com/other/repo.git"); e != nil {
			t.Fatalf("set pushurl: %s %v", out, e)
		}
		_, e := ResolveRemote(context.Background(), project2, policy("https://github.com/owner/repo.git"), "0123456789abcdef0123456789abcdef")
		if e == nil {
			t.Fatal("extra pushurl must be refused")
		}
	})

	t.Run("url rewrites are refused", func(t *testing.T) {
		if _, e := gitCfg(t, project, "--unset-all", "remote.origin.pushurl"); e != nil {
			t.Fatal(e)
		}
		if _, e := gitCfg(t, project, "url.https://evil.invalid/.insteadOf", "https://github.com/"); e != nil {
			t.Fatal(e)
		}
		_, e := ResolveRemote(context.Background(), project2, policy("https://github.com/owner/repo.git"), "0123456789abcdef0123456789abcdef")
		if e == nil {
			t.Fatal("url rewrite must be refused")
		}
	})

	t.Run("remote changed after authorization is refused", func(t *testing.T) {
		if _, e := gitCfg(t, project, "--unset-all", "url.https://evil.invalid/.insteadOf"); e != nil {
			t.Fatal(e)
		}
		if _, e := gitCfg(t, project, "remote.origin.url", "https://github.com/someoneelse/repo.git"); e != nil {
			t.Fatal(e)
		}
		_, e := ResolveRemote(context.Background(), project2, policy("https://github.com/owner/repo.git"), "0123456789abcdef0123456789abcdef")
		if e == nil {
			t.Fatal("changed remote must be refused")
		}
		if _, e := gitCfg(t, project, "remote.origin.url", "https://github.com/owner/repo.git"); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("non branch refs are refused for push", func(t *testing.T) {
		target, e := ResolveRemote(context.Background(), project2, policy("https://github.com/owner/repo.git"), "0123456789abcdef0123456789abcdef")
		if e != nil {
			t.Fatal(e)
		}
		runner := NewRemoteRunner(RemoteConfig{Resolver: resolver})
		for _, bad := range []string{"refs/tags/v1", "refs/heads/", "", "HEAD", "refs/kaiwu/x"} {
			if _, e := runner.Push(context.Background(), project, target, testSHA5, bad, "server_github_https", nil); e == nil {
				t.Fatalf("ref %q must be refused", bad)
			}
		}
	})
}

// TestDeliveryPushUnknownReceipt: a push that was accepted but whose receipt
// was lost is provable by Observe, and never re-pushed.
func TestDeliveryPushUnknownReceipt(t *testing.T) {
	root := t.TempDir()
	work := filepathJoin(t, root, "work")
	bare := filepathJoin(t, root, "bare.git")
	if out, e := runGit(t, work, "init", "-q"); e != nil {
		t.Fatalf("init: %s %v", out, e)
	}
	if out, e := runGit(t, bare, "init", "-q", "--bare"); e != nil {
		t.Fatalf("init bare: %s %v", out, e)
	}
	writeFile(t, work, "file.txt", "hello\n")
	_, e := runGit(t, work, "add", "--all")
	_, e = runGitEnv(t, work, envAuthor(), "commit", "-m", "task work", "--no-gpg-sign")
	if e != nil {
		t.Fatalf("commit: %v", e)
	}
	sha, e := gitopsHead(t, work)
	if e != nil {
		t.Fatal(e)
	}

	// Recording transport: maps the frozen URL to the local bare remote and
	// counts pushes; the first push "loses" its receipt.
	pushes := 0
	transport := RemoteTransportFunc(func(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
		mapped := make([]string, 0, len(args))
		for _, a := range args {
			if a == target.URL {
				a = bare
			}
			mapped = append(mapped, a)
		}
		if len(args) > 0 && args[0] == "push" {
			pushes++
			out, e := runGitEnv(t, dir, envAuthor(), mapped...)
			if e != nil {
				return out, e
			}
			if pushes == 1 {
				return "", context.DeadlineExceeded // receipt lost
			}
			return out, nil
		}
		return runGit(t, dir, mapped...)
	})
	runner := NewRemoteRunner(RemoteConfig{Transport: transport})
	target := RemoteTarget{RepositoryID: 123, URL: "https://github.com/owner/repo.git", MainRef: "main", TaskRef: "refs/heads/kaiwu/task-1"}

	t.Run("unknown push receipt is resolved by observation", func(t *testing.T) {
		_, e := runner.Push(context.Background(), work, target, sha, target.TaskRef, "server_github_https", nil)
		if e == nil {
			t.Fatal("lost receipt must surface as unknown, not success")
		}
		remoteSHA, e := runner.Observe(context.Background(), work, target, "refs/heads/kaiwu/task-1", "", nil)
		if e != nil {
			t.Fatalf("Observe: %v", e)
		}
		if remoteSHA != sha {
			t.Fatalf("remote sha %q, want %q", remoteSHA, sha)
		}
		// No retry: observation proved the result.
		if pushes != 1 {
			t.Fatalf("push count %d after confirmed receipt", pushes)
		}
	})

	t.Run("non fast forward is refused and remote preserved", func(t *testing.T) {
		// A competing commit on the remote: a descendant so the setup push
		// itself succeeds, then the old sha must be refused.
		other := filepathJoin(t, root, "other")
		_, e := runGit(t, other, "init", "-q")
		_, _ = runGit(t, other, "fetch", bare, "refs/heads/kaiwu/task-1")
		_, _ = runGit(t, other, "checkout", "-q", "-b", "competing", "FETCH_HEAD")
		writeFile(t, other, "competing.txt", "x\n")
		_, _ = runGit(t, other, "add", "--all")
		_, _ = runGitEnv(t, other, envAuthor(), "commit", "-m", "competing", "--no-gpg-sign")
		competing, _ := gitopsHead(t, other)
		if _, e := runner.Push(context.Background(), other, target, competing, target.TaskRef, "", nil); e != nil {
			t.Fatalf("competing push setup: %v", e)
		}
		remoteSHA, e := runner.Observe(context.Background(), work, target, "refs/heads/kaiwu/task-1", "", nil)
		if e != nil || remoteSHA != competing {
			t.Fatalf("observe after competing push: %q %v", remoteSHA, e)
		}
		if _, e := runner.Push(context.Background(), work, target, sha, target.TaskRef, "", nil); e == nil {
			t.Fatal("non fast forward push must be refused")
		}
		remoteSHA, _ = runner.Observe(context.Background(), work, target, "refs/heads/kaiwu/task-1", "", nil)
		if remoteSHA != competing {
			t.Fatal("refused push must not change the remote")
		}
	})

	t.Run("readable but unwritable remote reports permission failure", func(t *testing.T) {
		ro := filepathJoin(t, root, "ro.git")
		_, e := runGit(t, ro, "init", "-q", "--bare")
		if e != nil {
			t.Fatal(e)
		}
		roTarget := RemoteTarget{RepositoryID: 123, URL: "https://github.com/owner/repo.git", MainRef: "main"}
		pushBlocked := false
		readOnly := RemoteTransportFunc(func(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
			mapped := make([]string, 0, len(args))
			for _, a := range args {
				if a == target.URL {
					a = ro
				}
				mapped = append(mapped, a)
			}
			if len(mapped) > 0 && mapped[0] == "push" {
				pushBlocked = true
				return "", errors.New("remote: Permission denied")
			}
			return runGit(t, dir, mapped...)
		})
		roRunner := NewRemoteRunner(RemoteConfig{Transport: readOnly})
		remoteSHA, e := roRunner.Observe(context.Background(), work, roTarget, "refs/heads/kaiwu/task-1", "", nil)
		if e != nil {
			t.Fatalf("read must work: %v", e)
		}
		_ = remoteSHA
		if _, e := roRunner.Push(context.Background(), work, roTarget, sha, "refs/heads/kaiwu/task-1", "", nil); e == nil {
			t.Fatal("unwritable remote push must fail")
		}
		if !pushBlocked || len(remoteSHA) > 0 {
			t.Fatalf("push must be issued exactly once and observe empty: blocked=%v sha=%q", pushBlocked, remoteSHA)
		}
	})
}
