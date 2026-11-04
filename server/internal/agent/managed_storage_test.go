package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managedFixture(t *testing.T) (*Manager, workbench.Project) {
	t.Helper()
	root := t.TempDir()
	s, err := workbench.Open(filepath.Join(root, "global", "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r, err := s.CreateManagedProject(workbench.Command{Type: "create_project", OperationID: "managed-fixture", Name: "托管", Revision: s.Snapshot().Revision}, filepath.Join(root, "projects"), func(p workbench.Project) error {
		_, err := projectstorage.Create(context.Background(), filepath.Join(root, "projects"), p.ID, p.DirectoryName)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), s, Config{DataDir: filepath.Join(root, "global")})
	t.Cleanup(func() { m.Close() })
	return m, s.Snapshot().Projects[r.CreatedID]
}
func TestManagedLaunchAndWorktreeUseProjectNotGlobalRoots(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-unrelated")
	t.Setenv("ANTHROPIC_API_KEY", "synthetic-unrelated")
	m, p := managedFixture(t)
	c := m.store.Snapshot().Conversations[p.ConversationID]
	launch, err := m.launch(c)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(p.StorageRoot, ".kaiwu", "sessions", c.ID)
	if launch.Dir != want {
		t.Fatalf("managed discussion launched in global storage: %s, want %s", launch.Dir, want)
	}
	for _, env := range launch.Env {
		if strings.HasPrefix(env, "OPENAI_API_KEY=") || strings.HasPrefix(env, "ANTHROPIC_API_KEY=") {
			t.Fatal("ambient credentials leaked to managed project")
		}
	}
	for _, env := range launch.Env {
		if strings.HasPrefix(env, "HOME=") && env != "HOME="+filepath.Join(want, "home") {
			t.Fatal("shared HOME", env)
		}
	}
	if err = m.validateLocation(c, filepath.Join(m.cfg.DataDir, "sessions", c.ID, "fake.jsonl")); err == nil {
		t.Fatal("managed restore accepted legacy location")
	}
	taskID := strings.Repeat("b", 32)
	_, err = m.store.Command(workbench.Command{Type: "add_task", OperationID: "managed-task", ProjectID: p.ID, Request: "test", Revision: m.store.Snapshot().Revision})
	if err != nil {
		t.Fatal(err)
	}
	task := workbench.Task{ID: taskID, ProjectID: p.ID, ConversationID: "test-conversation"}
	if err = m.store.Update(func(st *workbench.State) error { st.Tasks[task.ID] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	x, err := m.prepareWorkspace(context.Background(), task, workbench.Execution{ID: "x", TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	if x.Workspace != filepath.Join(p.StorageRoot, "worktrees", taskID, "tree") {
		t.Fatal("worktree not project-owned", x.Workspace)
	}
	if _, err = os.Stat(filepath.Join(m.cfg.DataDir, "worktrees", taskID)); !os.IsNotExist(err) {
		t.Fatal("created global task worktree")
	}
	again, err := m.prepareWorkspace(context.Background(), task, x)
	if err != nil || again.Workspace != x.Workspace {
		t.Fatal("managed workspace recovery changed identity", err)
	}
}
