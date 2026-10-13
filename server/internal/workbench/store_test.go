package workbench

import (
	"path/filepath"
	"testing"
)

func TestPersistentBoardAndIndependentConversations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path, Profile{Provider: "test", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(c Command) Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := apply(Command{OperationID: "project-key", Type: "add_project", Name: "测试", Path: t.TempDir()}).CreatedID
	topic := apply(Command{OperationID: "topic-key", Type: "add_topic", ProjectID: project, Name: "功能"}).CreatedID
	task := apply(Command{OperationID: "task-key", Type: "add_task", ProjectID: project, TopicID: topic, Title: "卡片", Request: "需求"}).CreatedID
	before := s.Snapshot()
	var conv string
	for id, c := range before.Conversations {
		if c.TaskID == task {
			conv = id
		}
	}
	if conv == "" {
		t.Fatal("missing task conversation")
	}
	apply(Command{OperationID: "delete-key", Type: "delete_topic", ProjectID: project, TopicID: topic})
	after := s.Snapshot()
	if after.Tasks[task].TopicID != project+"_other" || after.Conversations[conv].TaskID != task {
		t.Fatal("delete lost task or binding")
	}
	r, e := s.Command(Command{OperationID: "task-key", Type: "add_task", ProjectID: project, TopicID: topic, Title: "卡片", Request: "需求", Revision: before.Revision - 1})
	if e != nil || r.CreatedID != task {
		t.Fatalf("retry must return original: %+v %v", r, e)
	}
	if _, e = s.Command(Command{OperationID: "task-key", Type: "add_task", ProjectID: project, Request: "different"}); e == nil {
		t.Fatal("same key with other payload")
	}
	if _, e = s.Command(Command{OperationID: "bad-key", Type: "delete_topic", ProjectID: "foreign", TopicID: project + "_other", Revision: after.Revision}); e == nil {
		t.Fatal("foreign scope accepted")
	}
	if _, e = s.Command(Command{OperationID: "stale-key", Type: "add_topic", ProjectID: project, Name: "stale", Revision: 0}); e == nil {
		t.Fatal("stale version accepted")
	}
	send := Command{OperationID: "send-key", Type: "send_message", ProjectID: project, TaskID: task, ConversationID: conv, Text: "独立消息", Revision: s.Snapshot().Revision}
	sent, e := s.Command(send)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.Command(send)
	if e != nil || !replay.Replay || replay.CreatedID != sent.CreatedID || len(s.Snapshot().Conversations[conv].Messages) != 1 {
		t.Fatal("send replay duplicated delivery")
	}
	if _, e = s.Command(Command{OperationID: "busy-send", Type: "send_message", ProjectID: project, TaskID: task, ConversationID: conv, Text: "must wait"}); e == nil {
		t.Fatal("busy conversation accepted second prompt")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if len(reopened.Snapshot().Tasks) != 1 || reopened.Snapshot().Conversations[conv].TaskID != task {
		t.Fatal("restart lost state")
	}
	restored := reopened.Snapshot().Conversations[conv]
	if restored.RunState != "interrupted" || restored.Messages[0].Delivery != "not_sent" || restored.Profile.Model != "fake" {
		t.Fatal("restart replayed work or changed frozen profile")
	}
}
