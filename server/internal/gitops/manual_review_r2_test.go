package gitops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// F1: a local merge that is already committed does not make newly written,
// uncommitted work integrate itself. PushOnly must stay false while anything is
// still pending, so the caller cannot skip the commit+merge step.
func TestReviewRemotePushOnlyRequiresNoPendingWork(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	if e := os.WriteFile(filepath.Join(f.x.Workspace, "late.txt"), []byte("late\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	local := reviewLocal(t, f)
	if !local.NeedsCommit || len(local.Files) == 0 {
		t.Fatalf("fixture is not dirty: %#v", local)
	}
	review, e := reviewLoop(t, f, localTransport(t, f.bare), true, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if !review.AlreadyMerged {
		t.Fatalf("committed task history should still be reported as merged: %#v", review)
	}
	if review.PushOnly {
		t.Fatalf("FINDING: PushOnly=true while %d uncommitted file(s) remain unmerged", len(local.Files))
	}
	if !review.CanPush || len(review.Pending) == 0 || review.PendingNote == "" {
		t.Fatalf("pending work not disclosed: %#v", review)
	}
}

// F2: losing the remote credential must not erase the local fact that the task
// is already integrated.
func TestReviewRemoteBlockedKeepsLocalMergedFact(t *testing.T) {
	f := reviewFixture(t)
	g(t, f.project.Path, "merge", "--ff-only", f.x.Branch)
	review, e := reviewLoop(t, f, localTransport(t, f.bare), false, "round-1")
	if e != nil {
		t.Fatal(e)
	}
	if review.CanPush || !strings.Contains(review.PushReason, "凭据") {
		t.Fatalf("credential blocking lost: %#v", review)
	}
	if !review.AlreadyMerged || !review.PushOnly {
		t.Fatalf("blocked review lost the local merged fact: %#v", review)
	}
}

// F3: a repository-local credential helper is attacker-controlled config. It
// must be rejected for manual review, whether generic, URL-scoped or pulled in
// through an include file.
func TestResolveManualRemoteRejectsRepoCredentialHelper(t *testing.T) {
	script := "!f() { :; }; f"
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, f reviewProject)
	}{
		{"generic", func(t *testing.T, f reviewProject) {
			g(t, f.project.Path, "config", "credential.helper", script)
		}},
		{"url scoped", func(t *testing.T, f reviewProject) {
			g(t, f.project.Path, "config", "credential.https://github.com.helper", script)
		}},
		{"include", func(t *testing.T, f reviewProject) {
			extra := filepath.Join(f.project.Path, ".git", "extra.conf")
			if e := os.WriteFile(extra, []byte("[credential \"https://github.com\"]\n\thelper = "+script+"\n"), 0o600); e != nil {
				t.Fatal(e)
			}
			g(t, f.project.Path, "config", "include.path", "extra.conf")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := reviewFixture(t)
			c.setup(t, f)
			if _, e := ResolveManualRemote(context.Background(), f.project); e == nil || !strings.Contains(e.Error(), "credential helper") {
				t.Fatalf("FINDING: repo credential helper not rejected: %v", e)
			}
		})
	}
}

// F3: the credential-fill mechanism itself must not execute a repository script,
// and a real 401 must fail closed instead of prompting through that helper.
func TestRemoteExecutionIgnoresRepoCredentialHelper(t *testing.T) {
	f := reviewFixture(t)
	marker := filepath.Join(t.TempDir(), "helper-ran")
	script := "!f() { touch " + marker + "; }; f"
	g(t, f.project.Path, "config", "credential.helper", script)
	g(t, f.project.Path, "config", "credential.http://127.0.0.1.helper", script)
	extra := filepath.Join(f.project.Path, ".git", "extra.conf")
	if e := os.WriteFile(extra, []byte("[credential \"http://127.0.0.1\"]\n\thelper = "+script+"\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, f.project.Path, "config", "include.path", "extra.conf")

	fill := exec.Command("git", Args(f.project.Path, "credential", "fill")...)
	fill.Env = Env()
	fill.Stdin = strings.NewReader("protocol=http\nhost=127.0.0.1\n\n")
	_, fillErr := fill.CombinedOutput()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	// This low-level credential isolation test deliberately uses a loopback
	// 401. Production remote entry rejects non-GitHub URLs before spawning Git.
	cmd := exec.Command("git", Args(f.x.Workspace, "ls-remote", server.URL+"/o/r.git", "refs/heads/main")...)
	cmd.Env = remoteEnv()
	_, e := cmd.CombinedOutput()
	if e == nil {
		t.Fatal("401 was not reported as a failure")
	}
	if _, e := os.Stat(marker); e == nil {
		t.Fatalf("FINDING: repository credential helper executed (credential fill err=%v)", fillErr)
	}
}
