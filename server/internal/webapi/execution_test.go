package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type startCommander struct {
	sends int
	err   error
}

func (c *startCommander) Send(string, string) error { c.sends++; return c.err }
func (c *startCommander) Resume(string) error       { return nil }
func (c *startCommander) AnswerInteraction(string, string) error {
	return nil
}

func TestStartAPIUsesOneDispatchAndReportsRefusals(t *testing.T) {
	if !pirpc.ProcessGroupsSupported() {
		t.Skip("execution platform unavailable")
	}
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "test", Model: "test"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cmd := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := cmd(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
	a := cmd(workbench.Command{OperationID: "task-a", Type: "add_task", ProjectID: p, Request: "A"}).CreatedID
	b := cmd(workbench.Command{OperationID: "task-b", Type: "add_task", ProjectID: p, Request: "B"}).CreatedID
	controller := &startCommander{}
	h := New(Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331"}, s, controller).(*handler)
	call := func(c workbench.Command, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(c)
		r := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/commands", strings.NewReader(string(data)))
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		r.Header.Set("X-Kaiwu-Client", "web")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/login", strings.NewReader(`{"token":"`+strings.Repeat("x", 32)+`"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.login(w, r)
	cookie := w.Result().Cookies()[0]
	c := workbench.Command{OperationID: "start-a", Type: "start_task", ProjectID: p, TaskID: a, ConversationID: s.Snapshot().Tasks[a].ConversationID}
	if w = call(c, cookie); w.Code != 503 || s.Snapshot().Tasks[a].Status != "idle" || controller.sends != 0 {
		t.Fatal("missing configuration changed task", w.Code)
	}
	h.cfg.AgentAvailable = true
	for i := 0; i < 2; i++ {
		if w = call(c, cookie); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if controller.sends != 1 {
		t.Fatal("receipt replay duplicated dispatch")
	}
	c.OperationID = "again-a"
	if w = call(c, cookie); w.Code != 409 || controller.sends != 1 {
		t.Fatal("new operation bypassed task guard")
	}
	controller.err = errors.New("capacity reached")
	chat := workbench.Command{OperationID: "start-chat", Type: "send_message", ProjectID: p, ConversationID: s.Snapshot().Tasks[b].ConversationID, Text: " 开始开发！ "}
	if w = call(chat, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	task := s.Snapshot().Tasks[b]
	if task.Status != "failed" || s.Snapshot().Executions[task.ExecutionID].Phase != "failed" {
		t.Fatal("failed dispatch left task running")
	}
	chat.OperationID = "project-start"
	chat.ConversationID = s.Snapshot().Projects[p].ConversationID
	if w = call(chat, cookie); w.Code != 400 || controller.sends != 2 {
		t.Fatal("project start chose arbitrary task", w.Code)
	}
}
