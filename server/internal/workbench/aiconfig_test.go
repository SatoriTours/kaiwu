package workbench

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/aiconfig"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"path/filepath"
	"testing"
)

func TestManagedTaskFreezesFileConfigAndKeepsEarlierSnapshot(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "data", "db"), Profile{Provider: "fake", Model: "old"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ConfigureAI(filepath.Join(root, "config")); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateManagedProject(Command{Type: "create_project", OperationID: "config-project", Name: "配置", Revision: s.Snapshot().Revision}, filepath.Join(root, "projects"), func(p Project) error {
		_, err := projectstorage.Create(context.Background(), filepath.Join(root, "projects"), p.ID, p.DirectoryName)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	add := func(op string) Task {
		t.Helper()
		r, err := s.Command(Command{Type: "add_task", OperationID: op, ProjectID: r.CreatedID, Request: "要求", Revision: s.Snapshot().Revision})
		if err != nil {
			t.Fatal(err)
		}
		return s.Snapshot().Tasks[r.CreatedID]
	}
	first := add("first-config")
	if first.ConfigSnapshot == nil || first.ConfigSnapshot.Profile.Model != "old" {
		t.Fatal("missing frozen config", first)
	}
	view, err := s.AIConfig("")
	if err != nil {
		t.Fatal(err)
	}
	next := aiconfig.FromProfile(Profile{Provider: "fake", Model: "new"})
	if _, err = s.SaveAIConfig("", view.Version, next); err != nil {
		t.Fatal(err)
	}
	second := add("second-config")
	st := s.Snapshot()
	if second.ConfigSnapshot.Profile.Model != "new" || st.Tasks[first.ID].ConfigSnapshot.Profile.Model != "old" || first.Profile.Model != "old" || st.Conversations[first.ConversationID].Profile.Model != "old" {
		t.Fatal("new template rewrote old task")
	}
	if _, err = s.SaveAIConfig("", view.Version, next); err == nil {
		t.Fatal("stale config overwrote file")
	}
	// Out-of-band admin file edits remain the source, not SQLite's profile projection.
	global, v, err := aiconfig.Read(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	global.Profile.Model = strptrConfig("file-edited")
	if _, err := aiconfig.Write(filepath.Join(root, "config"), v, global); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Profile.Model != "file-edited" || add("third-config").Profile.Model != "file-edited" {
		t.Fatal("SQLite became a second editable source")
	}

}

func strptrConfig(s string) *string { return &s }
