package workbench

import (
	"path/filepath"
	"testing"
)

func TestWebModelChoiceSnapshotsAndRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.db")
	s, e := Open(file, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	seq := 0
	command := func(c Command) (Receipt, error) {
		seq++
		c.OperationID = "profile-" + string(rune('a'+seq))
		c.Revision = s.Snapshot().Revision
		return s.Command(c)
	}
	p, e := command(Command{Type: "add_project", Name: "project", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	project := s.Snapshot().Projects[p.CreatedID]
	first, e := command(Command{Type: "add_task", ProjectID: project.ID, Request: "first"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = command(Command{Type: "set_conversation_profile", ProjectID: project.ID, ConversationID: project.ConversationID, Provider: "a", Model: "one"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = command(Command{Type: "set_default_profile", Provider: "b", Model: "two", Thinking: "high"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := command(Command{Type: "add_task", ProjectID: project.ID, Request: "second"})
	if e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	if st.Tasks[first.CreatedID].Profile.Model != "" || st.Tasks[second.CreatedID].Profile.Model != "two" || st.Conversations[project.ConversationID].Profile.Model != "one" {
		t.Fatal("default changed an existing snapshot")
	}
	task := st.Tasks[first.CreatedID]
	_, e = command(Command{Type: "set_conversation_profile", ProjectID: project.ID, ConversationID: task.ConversationID, Provider: "a", Model: "three"})
	if e != nil {
		t.Fatal(e)
	}
	if s.Snapshot().Tasks[task.ID].Profile.Model != "three" {
		t.Fatal("task snapshot not updated")
	}
	_, e = command(Command{Type: "send_message", ProjectID: project.ID, ConversationID: task.ConversationID, Text: "hello"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = command(Command{Type: "set_conversation_profile", ProjectID: project.ID, ConversationID: task.ConversationID, Provider: "b", Model: "four"})
	if e == nil {
		t.Fatal("started session model changed")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s = nil
	s, e = Open(file, Profile{Provider: "env", Model: "override"})
	if e != nil {
		t.Fatal(e)
	}
	if st = s.Snapshot(); st.Profile.Model != "two" || st.Conversations[project.ConversationID].Profile.Model != "one" || st.Tasks[task.ID].Profile.Model != "three" || st.Profile.CredentialSource != "server_pi" {
		t.Fatal("web choice lost after restart")
	}
}
