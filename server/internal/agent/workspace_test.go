package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func workspaceGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}

func workspaceFixture(t *testing.T) (*Manager, workbench.Task, workbench.Execution, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "project")
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, source, "init", "--template=")
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	task := workbench.Task{ID: strings.Repeat("a", 32), ProjectID: "p", ConversationID: "conv"}
	x := workbench.Execution{ID: "execution", TaskID: task.ID, ConversationID: "conv"}
	if e = s.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: source}
		st.Tasks[task.ID] = task
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	m := New(context.Background(), s, Config{DataDir: filepath.Join(root, "data")})
	t.Cleanup(func() { m.Close() })
	return m, task, x, source
}

func TestDevelopmentWorktreeDoesNotCommitOrModifySource(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unborn", true: "committed"}[committed], func(t *testing.T) {
			m, task, x, source := workspaceFixture(t)
			if e := os.WriteFile(filepath.Join(source, "README.md"), []byte("original"), 0600); e != nil {
				t.Fatal(e)
			}
			if committed {
				workspaceGit(t, source, "add", "README.md")
				workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
			}
			_ = os.WriteFile(filepath.Join(source, ".env"), []byte("private"), 0600)
			got, e := m.prepareWorkspace(context.Background(), task, x)
			if e != nil {
				t.Fatal(e)
			}
			if got.Workspace == source || workspaceGit(t, got.Workspace, "rev-parse", "--show-toplevel") != got.Workspace {
				t.Fatal("not an independent worktree")
			}
			b, e := os.ReadFile(filepath.Join(got.Workspace, "README.md"))
			if e != nil || string(b) != "original" {
				t.Fatal("missing task input", e)
			}
			if _, e = os.Stat(filepath.Join(got.Workspace, ".env")); !os.IsNotExist(e) {
				t.Fatal("copied credentials")
			}
			if !committed && exec.Command("git", "-C", source, "rev-parse", "--verify", "HEAD").Run() == nil {
				t.Fatal("created automatic commit")
			}
			_ = os.WriteFile(filepath.Join(got.Workspace, "README.md"), []byte("task change"), 0600)
			b, _ = os.ReadFile(filepath.Join(source, "README.md"))
			if string(b) != "original" {
				t.Fatal("source modified")
			}
			again, e := m.prepareWorkspace(context.Background(), task, got)
			if e != nil || again.Workspace != got.Workspace {
				t.Fatal("recovery failed", e)
			}
			b, _ = os.ReadFile(filepath.Join(got.Workspace, "README.md"))
			if string(b) != "task change" {
				t.Fatal("recovery discarded task changes")
			}
		})
	}
}

func TestWorktreeRejectsDirtyBaselineAndEscapingSymlink(t *testing.T) {
	for _, mode := range []string{"dirty", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			m, task, x, source := workspaceFixture(t)
			if mode == "dirty" {
				_ = os.WriteFile(filepath.Join(source, "a.txt"), []byte("base"), 0600)
				workspaceGit(t, source, "add", "a.txt")
				workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
				_ = os.WriteFile(filepath.Join(source, "a.txt"), []byte("uncommitted"), 0600)
			} else {
				if e := os.Symlink(t.TempDir(), filepath.Join(source, "outside")); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := m.prepareWorkspace(context.Background(), task, x); e == nil {
				t.Fatal("unsafe project input accepted")
			}
		})
	}
}

func TestUnbornStagedInputAndPartialWorkspaceRecovery(t *testing.T) {
	m, task, x, source := workspaceFixture(t)
	_ = os.WriteFile(filepath.Join(source, "staged.txt"), []byte("staged input"), 0600)
	workspaceGit(t, source, "add", "staged.txt")
	x, e := m.prepareWorkspace(context.Background(), task, x)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(x.Workspace, "staged.txt")); e != nil || string(b) != "staged input" {
		t.Fatal("unborn staged input omitted", e)
	}
	ownerPath := filepath.Join(filepath.Dir(x.Workspace), "owner.json")
	var v map[string]any
	b, _ := os.ReadFile(ownerPath)
	_ = json.Unmarshal(b, &v)
	v["Ready"] = false
	b, _ = json.Marshal(v)
	_ = os.WriteFile(ownerPath, b, 0600)
	if _, e = m.prepareWorkspace(context.Background(), task, x); e == nil {
		t.Fatal("partially prepared workspace silently accepted")
	}
}
