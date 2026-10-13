package gitops

import (
	"context"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null"}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (workbench.Project, workbench.Task, workbench.Execution) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "project")
	os.Mkdir(source, 0700)
	git(t, source, "init", "--initial-branch=main", "--template=")
	os.WriteFile(filepath.Join(source, "base.txt"), []byte("base\n"), 0600)
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "base")
	base := git(t, source, "rev-parse", "HEAD")
	id := strings.Repeat("a", 32)
	branch := "kaiwu/task-" + id
	parent := filepath.Join(root, "worktrees", id)
	os.MkdirAll(parent, 0700)
	tree := filepath.Join(parent, "tree")
	git(t, source, "worktree", "add", "-b", branch, tree, base)
	owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
	return workbench.Project{ID: "p", Path: source, DefaultBranch: "main", MainSeen: true}, workbench.Task{ID: id, ProjectID: "p"}, workbench.Execution{Workspace: tree, Branch: branch, BaseSHA: base}
}
func TestDetectMain(t *testing.T) {
	p, _, _ := fixture(t)
	if b, e := DetectMain(context.Background(), p.Path); e != nil || b != "main" {
		t.Fatal(b, e)
	}
	git(t, p.Path, "checkout", "-b", "other")
	if b, _ := DetectMain(context.Background(), p.Path); b != "main" {
		t.Fatal(b)
	}
	p.MainBranchOverride = "missing"
	if _, e := ResolveMain(context.Background(), p); e == nil {
		t.Fatal("missing override accepted")
	}
}
func TestGitStatusTracksActualHistory(t *testing.T) {
	p, task, x := fixture(t)
	ctx := context.Background()
	check := func(want string) {
		t.Helper()
		s, e := Check(ctx, p, task, x)
		if e != nil || s.State != want {
			t.Fatal(s, e, "want", want)
		}
	}
	check("no_changes")
	os.WriteFile(filepath.Join(x.Workspace, "new.txt"), []byte("feature"), 0600)
	check("dirty")
	git(t, x.Workspace, "add", ".")
	git(t, x.Workspace, "commit", "-m", "feature")
	check("unmerged")
	git(t, p.Path, "merge", "--ff-only", x.Branch)
	check("merged")
	os.WriteFile(filepath.Join(x.Workspace, "new.txt"), []byte("next"), 0600)
	check("dirty")
	git(t, x.Workspace, "commit", "-am", "next")
	check("unmerged")
	git(t, p.Path, "reset", "--hard", x.BaseSHA)
	check("unmerged")
}
func TestGitStatusRejectsChangedOwnership(t *testing.T) {
	p, task, x := fixture(t)
	git(t, x.Workspace, "checkout", "-b", "wrong")
	s, _ := Check(context.Background(), p, task, x)
	if s.State != "unknown" || s.CanMerge {
		t.Fatal(s)
	}
}
func TestPreviewBindsContentAndScope(t *testing.T) {
	p, task, x := fixture(t)
	names := []string{"two words.txt", "line\nbreak.txt", "--flag.txt"}
	for _, n := range names {
		os.WriteFile(filepath.Join(x.Workspace, n), []byte("first"), 0600)
	}
	a, e := Preview(context.Background(), p, task, x)
	if e != nil || !a.CanMerge || len(a.Files) != 3 {
		t.Fatal(a, e)
	}
	os.WriteFile(filepath.Join(x.Workspace, names[0]), []byte("other"), 0600)
	b, e := Preview(context.Background(), p, task, x)
	if e != nil || a.Fingerprint == b.Fingerprint {
		t.Fatal("content unbound", e)
	}
	os.WriteFile(filepath.Join(x.Workspace, ".env"), []byte("private"), 0600)
	b, e = Preview(context.Background(), p, task, x)
	if e != nil || b.CanMerge || b.Reason == "" {
		t.Fatal("private file accepted", b, e)
	}
}
func TestGitInspectionDoesNotRunExternalPrograms(t *testing.T) {
	p, task, x := fixture(t)
	sentinel := filepath.Join(t.TempDir(), "ran")
	git(t, p.Path, "config", "filter.bad.clean", "touch "+sentinel)
	os.WriteFile(filepath.Join(x.Workspace, ".gitattributes"), []byte("* filter=bad\n"), 0600)
	a, e := Preview(context.Background(), p, task, x)
	if e != nil || a.CanMerge || a.Reason == "" {
		t.Fatal(a, e)
	}
	if _, e = os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("external filter ran")
	}
}
func TestPartialCherryPickAndSquashStayUnmerged(t *testing.T) {
	for _, mode := range []string{"partial", "squash"} {
		t.Run(mode, func(t *testing.T) {
			p, task, x := fixture(t)
			os.WriteFile(filepath.Join(x.Workspace, "one"), []byte("1"), 0600)
			git(t, x.Workspace, "add", ".")
			git(t, x.Workspace, "commit", "-m", "one")
			one := git(t, x.Workspace, "rev-parse", "HEAD")
			os.WriteFile(filepath.Join(x.Workspace, "two"), []byte("2"), 0600)
			git(t, x.Workspace, "add", ".")
			git(t, x.Workspace, "commit", "-m", "two")
			if mode == "partial" {
				git(t, p.Path, "cherry-pick", one)
			} else {
				git(t, p.Path, "merge", "--squash", x.Branch)
				git(t, p.Path, "commit", "-m", "squash")
			}
			s, e := Check(context.Background(), p, task, x)
			if e != nil || s.State != "unmerged" {
				t.Fatal(s, e)
			}
		})
	}
}
func TestCommittedPrivateResultBlocksMerge(t *testing.T) {
	p, task, x := fixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "secret.pem"), []byte("fixture only"), 0600)
	git(t, x.Workspace, "add", ".")
	git(t, x.Workspace, "commit", "-m", "external commit")
	s, e := Check(context.Background(), p, task, x)
	if e != nil || s.CanMerge || s.Reason == "" {
		t.Fatal("private committed result accepted", s, e)
	}
}

func TestDeletedPrivateHistoryBlocksMerge(t *testing.T) {
	p, task, x := fixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "secret.pem"), []byte("fixture only"), 0600)
	git(t, x.Workspace, "add", ".")
	git(t, x.Workspace, "commit", "-m", "private history")
	os.Remove(filepath.Join(x.Workspace, "secret.pem"))
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	git(t, x.Workspace, "add", "--all")
	git(t, x.Workspace, "commit", "-m", "remove private path")
	s, e := Check(context.Background(), p, task, x)
	if e != nil || s.CanMerge || s.Reason == "" {
		t.Fatal("private historical commit would enter main", s, e)
	}
}
