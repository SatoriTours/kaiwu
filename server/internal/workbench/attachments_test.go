package workbench

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentsDoNotBypassMessageBoundsOrStartIntent(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{Provider: "test", Model: "test"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Command(Command{Type: "add_project", OperationID: "attachment-project", Revision: s.Snapshot().Revision, Name: "test", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	items := []Attachment{{ID: "test", Name: "note", MIME: "text/plain", Size: 4}}
	if _, e = s.Command(Command{Type: "send_message", OperationID: "attachment-too-long", ProjectID: p.ID, ConversationID: p.ConversationID, Text: strings.Repeat("x", 5001), Attachments: items}); e == nil {
		t.Fatal("attachments bypassed text bound")
	}
	r, e = s.Command(Command{Type: "send_message", OperationID: "attachment-start-text", ProjectID: p.ID, ConversationID: p.ConversationID, Text: "开始开发", Attachments: items})
	if e != nil || r.Type != "send_message" {
		t.Fatal(r, e)
	}
	if len(s.Snapshot().Conversations[p.ConversationID].Messages[0].Attachments) != 1 {
		t.Fatal("attachments lost")
	}
}
