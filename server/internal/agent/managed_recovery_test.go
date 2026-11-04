package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/aiconfig"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"github.com/SatoriTours/kaiwu/server/internal/projecttools"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedFakePiConfigMaterialsAndRestartAreProjectOwned(t *testing.T) {
	m, p := managedFixture(t)
	s := m.store
	configRoot := filepath.Join(t.TempDir(), "config")
	if err := s.ConfigureAI(configRoot); err != nil {
		t.Fatal(err)
	}
	model := "original"
	skills := []string{"review"}
	credential := ""
	g := aiconfig.Config{SchemaVersion: 1, Profile: &aiconfig.ProfilePatch{Provider: strptr("fake"), Model: &model}, CredentialRef: &credential, Skills: &skills}
	skillSource := filepath.Join(configRoot, "skills", "review")
	if err := os.MkdirAll(skillSource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillSource, "SKILL.md"), []byte("---\nname: review\ndescription: fixture\n---\noriginal skill"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err := s.AIConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAIConfig("", view.Version, g); err != nil {
		t.Fatal(err)
	}
	projectsRoot := filepath.Dir(p.StorageRoot)
	r, err := s.CreateManagedProject(workbench.Command{Type: "create_project", OperationID: "second-project", Name: "第二", Revision: s.Snapshot().Revision}, projectsRoot, func(p workbench.Project) error {
		_, err := projectstorage.Create(context.Background(), projectsRoot, p.ID, p.DirectoryName)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	second := s.Snapshot().Projects[r.CreatedID]
	tasks := []workbench.Task{}
	for i, project := range []workbench.Project{p, second} {
		name := []string{"model-A", "model-B"}[i]
		rules := []string{"only-" + name}
		pv, err := s.AIConfig(project.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveAIConfig(project.ID, pv.Version, aiconfig.Config{SchemaVersion: 1, Profile: &aiconfig.ProfilePatch{Model: &name}, Rules: &rules}); err != nil {
			t.Fatal(err)
		}
		r, err := s.Command(workbench.Command{Type: "add_task", OperationID: "task-" + name, ProjectID: project.ID, Request: name, Revision: s.Snapshot().Revision})
		if err != nil {
			t.Fatal(err)
		}
		task := s.Snapshot().Tasks[r.CreatedID]
		tasks = append(tasks, task)
		c := s.Snapshot().Conversations[task.ConversationID]
		launch, err := m.launch(c)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(launch.Args, " ")
		if !strings.Contains(joined, "only-"+name) || !strings.Contains(joined, "--no-extensions") || !strings.Contains(joined, filepath.Join(project.StorageRoot, ".kaiwu", "snapshots", c.ID+".skills", "review", "SKILL.md")) {
			t.Fatal("launch lost frozen configuration", joined)
		}
		if c.ConfigSnapshot == nil || !strings.Contains(c.ConfigSnapshot.SkillContents["review"], "original skill") {
			t.Fatal("skill contents not frozen")
		}
	}
	if err := os.WriteFile(filepath.Join(skillSource, "SKILL.md"), []byte("changed template"), 0600); err != nil {
		t.Fatal(err)
	}
	// Changing files does not invalidate or rewrite already-frozen task skills.
	for _, task := range tasks {
		if _, err := m.launch(s.Snapshot().Conversations[task.ConversationID]); err != nil {
			t.Fatal(err)
		}
	}
	executable, _ := os.Executable()
	m.cfg.Program = executable
	m.cfg.BaseArgs = []string{"-test.run=TestFakePi", "--"}
	m.cfg.ExtraEnv = []string{"KAIWU_AGENT_HELPER=1"}
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, err := s.Command(c)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	wait := func(conv string) workbench.Conversation {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			c := s.Snapshot().Conversations[conv]
			if c.RunState == "failed" {
				t.Fatal(c.Error)
			}
			if c.RunState == "idle" {
				return c
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("fake pi did not settle")
		return workbench.Conversation{}
	}
	before := map[string]workbench.Conversation{}
	for _, task := range tasks {
		c := s.Snapshot().Conversations[task.ConversationID]
		r := command(workbench.Command{Type: "send_message", OperationID: "send-" + task.ID, ProjectID: task.ProjectID, ConversationID: c.ID, Text: "scope-" + task.ID})
		if err := m.Send(c.ID, r.CreatedID); err != nil {
			t.Fatal(err)
		}
		c = wait(c.ID)
		before[c.ID] = c
		project := s.Snapshot().Projects[task.ProjectID]
		if !strings.HasPrefix(c.SessionFile, filepath.Join(project.StorageRoot, ".kaiwu", "sessions", c.ID)+string(os.PathSeparator)) {
			t.Fatal("session escaped project", c.SessionFile)
		}
		l, err := projectstorage.ResolveProject(project.Storage(), m.cfg.DataDir)
		if err != nil {
			t.Fatal(err)
		}
		attachments := projecttools.Attachments{Dir: l.Attachments}
		a, err := attachments.Save(c.ID, "fixture.txt", strings.NewReader("project-private"))
		if err != nil {
			t.Fatal(err)
		}
		other := tasks[0]
		if other.ProjectID == task.ProjectID {
			other = tasks[1]
		}
		op := s.Snapshot().Projects[other.ProjectID]
		ol, _ := projectstorage.ResolveProject(op.Storage(), m.cfg.DataDir)
		if _, _, err := (projecttools.Attachments{Dir: ol.Attachments}).Load(other.ConversationID, a.ID); err == nil {
			t.Fatal("cross-project attachment accepted")
		}
	}
	first := tasks[0]
	r = command(workbench.Command{Type: "start_task", OperationID: "switch-worktree", ProjectID: first.ProjectID, TaskID: first.ID})
	if err := m.Send(first.ConversationID, r.CreatedID); err != nil {
		t.Fatal(err)
	}
	afterStart := wait(first.ConversationID)
	original := before[first.ConversationID]
	if afterStart.SessionID != original.SessionID || afterStart.SessionFile != original.SessionFile {
		t.Fatal("discussion/worktree switch replaced native session")
	}
	x := s.Snapshot().Executions[s.Snapshot().Tasks[first.ID].ExecutionID]
	if x.Workspace != filepath.Join(p.StorageRoot, "worktrees", first.ID, "tree") {
		t.Fatal("task workspace not project owned", x.Workspace)
	}
	before[first.ConversationID] = afterStart
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	data := m.cfg.DataDir
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := workbench.Open(filepath.Join(data, "db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.ConfigureAI(configRoot); err != nil {
		t.Fatal(err)
	}
	m2 := New(context.Background(), restored, m.cfg)
	defer m2.Close()
	for _, task := range tasks {
		c := restored.Snapshot().Conversations[task.ConversationID]
		prior := before[c.ID]
		rr, err := restored.Command(workbench.Command{Type: "send_message", OperationID: "after-restart-" + task.ID, ProjectID: task.ProjectID, ConversationID: c.ID, Text: "new intent", Revision: restored.Snapshot().Revision})
		if err != nil {
			t.Fatal(err)
		}
		if err = m2.Send(c.ID, rr.CreatedID); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			c = restored.Snapshot().Conversations[c.ID]
			if c.RunState == "idle" || c.RunState == "failed" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if c.RunState != "idle" || c.SessionID != prior.SessionID || c.SessionFile != prior.SessionFile || len(c.Messages) <= len(prior.Messages) {
			t.Fatal("restart lost original context", c.RunState, c.Error)
		}
	}
}
func strptr(s string) *string { return &s }
