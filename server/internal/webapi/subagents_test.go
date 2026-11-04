package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
)

type subagentAPI struct {
	store        *workbench.Store
	handler      http.Handler
	server       *httptest.Server
	host         string
	cookie       *http.Cookie
	project      string
	conversation string
	task         string
	round        string
	run          string
	secret       string
	eventSecret  string
}

func newSubagentAPI(t *testing.T) *subagentAPI {
	t.Helper()
	store, err := workbench.Open(filepath.Join(t.TempDir(), "api.db"), workbench.Profile{Provider: "test", Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = store.Snapshot().Revision
		r, e := store.Command(c)
		if e != nil {
			t.Fatalf("command %s: %v", c.Type, e)
		}
		return r
	}
	api := &subagentAPI{store: store}
	api.project = command(workbench.Command{OperationID: "api-project", Type: "add_project", Name: "接口", Path: t.TempDir()}).CreatedID
	api.task = command(workbench.Command{OperationID: "api-task", Type: "add_task", ProjectID: api.project, TopicID: api.project + "_other", Title: "卡片", Request: "需求"}).CreatedID
	for key, c := range store.Snapshot().Conversations {
		if c.TaskID == api.task {
			api.conversation = key
		}
	}
	if api.conversation == "" {
		t.Fatal("missing task conversation")
	}
	api.round = command(workbench.Command{OperationID: "api-round", Type: "send_message", ProjectID: api.project, TaskID: api.task, ConversationID: api.conversation, Text: "开始"}).CreatedID
	api.run = "run-api"
	api.secret = "SECRET-API-REQUEST-完整任务正文"
	api.eventSecret = "SECRET-API-EVENT-可见回复"
	summary := workbench.SubagentSummary{
		ID: api.run, ProjectID: api.project, ConversationID: api.conversation, TaskID: api.task,
		RoundID: api.round, ParentSessionID: store.Snapshot().Conversations[api.conversation].SessionID,
		Title: "检查接口", TaskSummary: "只读详情", Status: workbench.SubagentStatusStarting,
		Profile: workbench.Profile{Provider: "test", Model: "fake", Thinking: "high"},
	}
	if _, err := store.CreateSubagentRun(summary, api.secret); err != nil {
		t.Fatal(err)
	}
	for _, event := range []workbench.SubagentEvent{
		{ID: "ev-1", Kind: workbench.SubagentEventText, Text: api.eventSecret},
		{ID: "ev-2", Kind: workbench.SubagentEventToolStart, ToolID: "tool-a", ToolName: "read", Input: `{"path":"a.go"}`},
		{ID: "ev-3", Kind: workbench.SubagentEventToolEnd, ToolID: "tool-a", ToolName: "read", Output: "ok"},
		{ID: "ev-4", Kind: workbench.SubagentEventStatus, Status: workbench.SubagentStatusRunning},
		{ID: "ev-5", Kind: workbench.SubagentEventStatus, Status: workbench.SubagentStatusCompleted},
	} {
		if _, err := store.AppendSubagentEvent(api.conversation, api.run, event); err != nil {
			t.Fatal(err)
		}
	}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	api.host = parsed.Host
	handler = New(Config{Host: api.host, Token: strings.Repeat("x", 32)}, store, nil)
	api.handler, api.server = handler, server
	login := api.call("POST", "/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil, api.headers())
	if login.Code != 200 {
		t.Fatalf("login failed: %d %s", login.Code, login.Body.String())
	}
	api.cookie = login.Result().Cookies()[0]
	return api
}

func (a *subagentAPI) headers() map[string]string {
	return map[string]string{"Origin": "http://" + a.host, "X-Kaiwu-Client": "web"}
}

func (a *subagentAPI) call(method, path, body string, cookie *http.Cookie, extra map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+a.host+path, strings.NewReader(body))
	req.Host = a.host
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range extra {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, req)
	return w
}

func (a *subagentAPI) detailBody() string {
	return fmt.Sprintf(`{"projectId":%q,"conversationId":%q,"runId":%q}`, a.project, a.conversation, a.run)
}

func (a *subagentAPI) socket(t *testing.T) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, strings.Replace(a.server.URL, "http:", "ws:", 1)+"/api/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {a.server.URL}, "Cookie": {a.cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	conn.SetReadLimit(2 << 20)
	return conn
}

func socketSend(t *testing.T, conn *websocket.Conn, id, method string, body any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "body": body})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatal(err)
	}
}

func socketReadReply(t *testing.T, conn *websocket.Conn, id string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		switch value["type"] {
		case "snapshot", "snapshot_start", "snapshot_chunk", "snapshot_end", "patch":
			continue
		}
		if value["id"] == id {
			return value
		}
	}
}

func TestSubagentDetailHTTPAuthScopeAndBounds(t *testing.T) {
	api := newSubagentAPI(t)
	if w := api.call("POST", "/api/subagents/detail", api.detailBody(), nil, api.headers()); w.Code != 401 {
		t.Fatalf("unauthenticated detail accepted: %d", w.Code)
	}
	if w := api.call("POST", "/api/subagents/detail", api.detailBody(), api.cookie, nil); w.Code != 403 {
		t.Fatalf("missing same-origin proof accepted: %d", w.Code)
	}
	w := api.call("POST", "/api/subagents/detail", api.detailBody(), api.cookie, api.headers())
	if w.Code != 200 {
		t.Fatalf("detail refused: %d %s", w.Code, w.Body.String())
	}
	var page workbench.SubagentDetailPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Run.ID != api.run || page.Run.Status != workbench.SubagentStatusCompleted || page.Request != api.secret {
		t.Fatalf("detail identity wrong: %+v", page.Run)
	}
	if len(page.Events) != 5 || page.HasMore || page.Next != 5 {
		t.Fatalf("detail page wrong: %+v", page)
	}
	if page.Events[0].Kind != workbench.SubagentEventText || page.Events[0].At == "" || page.Events[1].ToolName != "read" || page.Events[2].Output != "ok" {
		t.Fatalf("detail event fields wrong: %+v", page.Events)
	}
	raw := w.Body.String()
	for _, field := range []string{`"hasMore"`, `"next"`, `"request"`, `"parentSessionId"`, `"taskSummary"`, `"truncated"`} {
		if !strings.Contains(raw, field) {
			t.Fatalf("detail page lost field %s: %s", field, raw)
		}
	}
	if strings.Contains(raw, `"subagents"`) {
		t.Fatalf("detail page must not carry the snapshot projection: %s", raw)
	}
	if w := api.call("POST", "/api/subagents/detail", api.detailBody()[:len(api.detailBody())-1]+`,"unexpected":1}`, api.cookie, api.headers()); w.Code != 400 {
		t.Fatalf("unknown field accepted: %d %s", w.Code, w.Body.String())
	}
	if w := api.call("POST", "/api/subagents/detail", `{"projectId":1}`, api.cookie, api.headers()); w.Code != 400 {
		t.Fatalf("wrong field type accepted: %d", w.Code)
	}
	cross := []string{
		fmt.Sprintf(`{"projectId":"other","conversationId":%q,"runId":%q}`, api.conversation, api.run),
		fmt.Sprintf(`{"projectId":%q,"conversationId":"other","runId":%q}`, api.project, api.run),
		fmt.Sprintf(`{"projectId":%q,"conversationId":%q,"runId":"other"}`, api.project, api.conversation),
	}
	for _, body := range cross {
		if w := api.call("POST", "/api/subagents/detail", body, api.cookie, api.headers()); w.Code != 404 {
			t.Fatalf("cross-scope detail accepted (%s): %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, limit := range []string{"-1", "101"} {
		body := fmt.Sprintf(`{"projectId":%q,"conversationId":%q,"runId":%q,"limit":%s}`, api.project, api.conversation, api.run, limit)
		if w := api.call("POST", "/api/subagents/detail", body, api.cookie, api.headers()); w.Code != 400 {
			t.Fatalf("limit %s accepted: %d", limit, w.Code)
		}
	}
	paged := fmt.Sprintf(`{"projectId":%q,"conversationId":%q,"runId":%q,"limit":2}`, api.project, api.conversation, api.run)
	if w := api.call("POST", "/api/subagents/detail", paged, api.cookie, api.headers()); w.Code != 200 || !strings.Contains(w.Body.String(), `"hasMore":true`) {
		t.Fatalf("paged detail wrong: %d %s", w.Code, w.Body.String())
	}
	if w := api.call("GET", "/api/subagents/detail", "", api.cookie, nil); w.Code != 404 {
		t.Fatalf("GET detail accepted: %d", w.Code)
	}
	for _, path := range []string{"/api/subagents/create", "/api/subagents/append", "/api/subagents/control", "/api/subagents/"} {
		if w := api.call("POST", path, "{}", api.cookie, api.headers()); w.Code != 404 {
			t.Fatalf("write path %s answered: %d", path, w.Code)
		}
	}
	before := len(api.store.Snapshot().Subagents)
	command := fmt.Sprintf(`{"operationId":"api-create-subagent","revision":%d,"type":"create_subagent","projectId":%q,"conversationId":%q,"title":"伪造"}`, api.store.Snapshot().Revision, api.project, api.conversation)
	if w := api.call("POST", "/api/commands", command, api.cookie, api.headers()); w.Code != 400 {
		t.Fatalf("public create command accepted: %d %s", w.Code, w.Body.String())
	}
	if len(api.store.Snapshot().Subagents) != before {
		t.Fatal("public command created a subagent run")
	}
	snapshot := api.call("GET", "/api/snapshot", "", api.cookie, nil)
	if snapshot.Code != 200 {
		t.Fatalf("snapshot failed: %d", snapshot.Code)
	}
	body := snapshot.Body.String()
	if !strings.Contains(body, `"run-api"`) || !strings.Contains(body, `"subagents"`) {
		t.Fatal("snapshot lost the lightweight summary")
	}
	if strings.Contains(body, api.secret) || strings.Contains(body, api.eventSecret) {
		t.Fatal("snapshot leaked request or event content")
	}
}

func TestSubagentDetailSocketMethod(t *testing.T) {
	api := newSubagentAPI(t)
	conn := api.socket(t)
	socketSend(t, conn, "detail-1", "subagents/detail", map[string]any{"projectId": api.project, "conversationId": api.conversation, "runId": api.run, "limit": 2})
	reply := socketReadReply(t, conn, "detail-1")
	if reply["status"] != float64(200) {
		t.Fatalf("socket detail refused: %+v", reply)
	}
	data, _ := json.Marshal(reply["data"])
	var page workbench.SubagentDetailPage
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Run.ID != api.run || len(page.Events) != 2 || !page.HasMore || page.Next != 2 || page.Request != api.secret {
		t.Fatalf("socket detail page wrong: %+v", page)
	}
	socketSend(t, conn, "detail-2", "subagents/detail", map[string]any{"projectId": "other", "conversationId": api.conversation, "runId": api.run})
	reply = socketReadReply(t, conn, "detail-2")
	if reply["status"] != float64(404) {
		t.Fatalf("socket cross-scope read accepted: %+v", reply)
	}
	errorBody, _ := json.Marshal(reply["data"])
	if !strings.Contains(string(errorBody), "not_found") {
		t.Fatalf("socket scope error lost its code: %s", errorBody)
	}
	socketSend(t, conn, "detail-3", "subagents/detail", map[string]any{"projectId": api.project, "conversationId": api.conversation, "runId": api.run, "extra": true})
	reply = socketReadReply(t, conn, "detail-3")
	if reply["status"] != float64(400) {
		t.Fatalf("socket unknown field accepted: %+v", reply)
	}
	before := len(api.store.Snapshot().Subagents)
	socketSend(t, conn, "detail-4", "subagents/create", map[string]any{"projectId": api.project})
	reply = socketReadReply(t, conn, "detail-4")
	if reply["status"] != float64(400) {
		t.Fatalf("socket write method answered: %+v", reply)
	}
	if len(api.store.Snapshot().Subagents) != before {
		t.Fatal("socket created a subagent run")
	}
}
