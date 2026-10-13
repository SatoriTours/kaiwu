package workbench

import (
	"path/filepath"
	"testing"
)

func TestMergeAdmissionAndReplay(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p"}
		st.Tasks["t"] = Task{ID: "t", ProjectID: "p", ConversationID: "c"}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle"}
		return nil
	})
	req := MergeRequest{OperationID: "merge-test", ProjectID: "p", TaskID: "t", PreviewID: "preview", CommitMessage: "feature"}
	proof := MergePreview{ID: "preview", ProjectID: "p", TaskID: "t", CanMerge: true}
	op, replay, e := s.BeginMerge(req, proof)
	if e != nil || replay || op.ID != req.OperationID {
		t.Fatal(op, replay, e)
	}
	_, replay, e = s.BeginMerge(req, proof)
	if e != nil || !replay {
		t.Fatal("not replay", e)
	}
	if _, e = s.Command(Command{OperationID: "settings-active", Type: "update_project_git", Revision: s.Snapshot().Revision, ProjectID: "p", CommitName: "Changed"}); e == nil {
		t.Fatal("project settings changed during merge")
	}
	req.CommitMessage = "different"
	if _, _, e = s.BeginMerge(req, proof); e == nil {
		t.Fatal("different payload accepted")
	}
	if _, e = s.Command(Command{OperationID: "send-test", Type: "send_message", ProjectID: "p", ConversationID: "c", Text: "hi"}); e == nil {
		t.Fatal("writer accepted during merge")
	}
}
func TestMergeRestartNeverReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Update(func(st *State) error {
		st.Merges["m"] = MergeOperation{ID: "m", Phase: "merging", TaskAfterSHA: "abc"}
		return nil
	})
	_ = s.Close()
	s, e = Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got := s.Snapshot()
	if got.Merges["m"].Phase != "needs_check" || got.Merges["m"].TaskAfterSHA != "abc" {
		t.Fatal(got.Merges)
	}
}
func TestUnchangedGitStatusDoesNotBumpRevision(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.Snapshot().TaskGit == nil || s.Snapshot().Merges == nil {
		t.Fatal("missing maps")
	}
	status := GitStatus{TaskID: "t", State: "no_changes", CheckedAt: "first"}
	if e = s.SaveGitStatus(status); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot().Revision
	status.CheckedAt = "second"
	if e = s.SaveGitStatus(status); e != nil {
		t.Fatal(e)
	}
	if s.Snapshot().Revision != before {
		t.Fatal("unchanged status bumped revision")
	}
}
func TestGitStateBackwardCompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.Exec(`UPDATE state SET body='{"revision":1,"projects":{},"topics":[],"tasks":{},"conversations":{},"profile":{}}',revision=1`)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if st := s.Snapshot(); st.TaskGit == nil || st.Merges == nil || st.Executions == nil {
		t.Fatal("legacy JSON maps not migrated")
	}
}
func TestGitChecksDoNotInvalidateBoardCommands(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Command(Command{OperationID: "project-board", Revision: s.Snapshot().Revision, Type: "add_project", Name: "p", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	_ = s.SaveGitStatus(GitStatus{TaskID: "t", State: "dirty"})
	revision := before.StructureRevision
	c := Command{OperationID: "topic-board", Revision: before.Revision, StructureRevision: &revision, Type: "add_topic", ProjectID: p.CreatedID, Name: "topic"}
	if _, e = s.Command(c); e != nil {
		t.Fatal("Git inspection invalidated board form", e)
	}
	c.OperationID = "topic-stale"
	c.Name = "stale"
	if _, e = s.Command(c); e == nil {
		t.Fatal("actual structural edit did not invalidate stale form")
	}
}
