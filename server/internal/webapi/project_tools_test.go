package webapi

import (
	"bytes"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsAuthenticatedScopeAndAttachments(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "test", Model: "test"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0600)
	receipt, e := s.Command(workbench.Command{Type: "add_project", OperationID: "tools-test-project", Revision: s.Snapshot().Revision, Name: "project", Path: dir})
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot().Projects[receipt.CreatedID]
	token := strings.Repeat("t", 32)
	h := New(Config{Host: "127.0.0.1:7331", Token: token, DataDir: t.TempDir(), AgentAvailable: true}, s, nil)
	defer h.(*handler).Close()
	call := func(method, path string, body []byte, content string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331/api/"+path, bytes.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		r.Header.Set("X-Kaiwu-Client", "web")
		r.Header.Set("Content-Type", content)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := call("POST", "login", []byte(`{"token":"`+token+`"}`), "application/json", nil)
	cookie := login.Result().Cookies()[0]
	q := "tools/files?projectId=" + p.ID + "&path=."
	if w := call("GET", q, nil, "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("GET", q, nil, "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "note.txt") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "tools/file?projectId="+p.ID+"&path=../outside", nil, "", cookie); w.Code == 200 {
		t.Fatal("traversal")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "note.txt")
	part.Write([]byte("attachment"))
	mw.Close()
	w := call("POST", "attachments?conversationId="+p.ConversationID, body.Bytes(), mw.FormDataContentType(), cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var a workbench.Attachment
	if json.Unmarshal(w.Body.Bytes(), &a) != nil || a.ID == "" {
		t.Fatal(w.Body.String())
	}
	if w = call("GET", "attachments/"+a.ID+"?conversationId=other", nil, "", cookie); w.Code == 200 {
		t.Fatal("cross scope")
	}
	cmd := map[string]any{"operationId": "send-attachment-test", "revision": 0, "type": "send_message", "projectId": p.ID, "conversationId": p.ConversationID, "text": "", "attachments": []workbench.Attachment{a}}
	b, _ := json.Marshal(cmd)
	w = call("POST", "commands", b, "application/json", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	messages := s.Snapshot().Conversations[p.ConversationID].Messages
	if len(messages) != 1 || len(messages[0].Attachments) != 1 {
		t.Fatal(messages)
	}
}
