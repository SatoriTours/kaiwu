package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type interactionCommander struct {
	mu        sync.Mutex
	sends     []string
	answers   []string
	sendErr   error
	answerErr error
}

func (c *interactionCommander) Send(conversationID, messageID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sends = append(c.sends, conversationID+":"+messageID)
	return c.sendErr
}

func (c *interactionCommander) Resume(string) error { return nil }

func (c *interactionCommander) AnswerInteraction(conversationID, interactionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.answers = append(c.answers, conversationID+":"+interactionID)
	return c.answerErr
}

func (c *interactionCommander) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sends), len(c.answers)
}

type interactionAPI struct {
	t       *testing.T
	h       *handler
	store   *workbench.Store
	conv    string
	task    string
	project string
	cookie  *http.Cookie
}

func setupInteractionAPI(t *testing.T, commander Commander) *interactionAPI {
	t.Helper()
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	command := func(c workbench.Command) workbench.Receipt {
		t.Helper()
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := command(workbench.Command{OperationID: "api-project", Type: "add_project", Name: "交互", Path: t.TempDir()}).CreatedID
	task := command(workbench.Command{OperationID: "api-task", Type: "add_task", ProjectID: project, Title: "交互", Request: "实现"}).CreatedID
	conv := s.Snapshot().Tasks[task].ConversationID
	_ = s.Update(func(st *workbench.State) error {
		c := st.Conversations[conv]
		c.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		st.Conversations[conv] = c
		return nil
	})
	token := strings.Repeat("x", 32)
	h := New(Config{Token: token, Host: "127.0.0.1:7331", AgentAvailable: true}, s, commander).(*handler)
	api := &interactionAPI{t: t, h: h, store: s, conv: conv, task: task, project: project}
	api.login(token)
	return api
}

func (a *interactionAPI) login(token string) {
	a.t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/login", strings.NewReader(`{"token":"`+token+`"}`))
	r.Header.Set("Origin", "http://127.0.0.1:7331")
	r.Header.Set("X-Kaiwu-Client", "web")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, r)
	if w.Code != 200 {
		a.t.Fatalf("login %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		a.t.Fatal("missing cookie")
	}
	a.cookie = cookies[0]
}

func (a *interactionAPI) call(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://127.0.0.1:7331")
	r.Header.Set("X-Kaiwu-Client", "web")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(a.cookie)
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, r)
	return w
}

func (a *interactionAPI) seed(request workbench.InteractionRequest) workbench.InteractionRequest {
	a.t.Helper()
	request.SchemaVersion = workbench.InteractionSchemaVersion
	request.Version = 1
	request.ProjectID = a.project
	request.TaskID = a.task
	request.ConversationID = a.conv
	request.RoundID = "round-api"
	request.CreatedAt = "2026-10-18T10:00:00Z"
	request.Blocking = true
	request.AllowCancel = true
	if request.Source == "" {
		request.Source = workbench.SourceAssistantQuestion
	}
	if request.ID == "" {
		request.ID = "ix_api"
	}
	if request.Title == "" {
		request.Title = "需要确认"
	}
	if len(request.Fields) == 0 {
		request.Fields = []workbench.InteractionField{{ID: "allow", Type: workbench.FieldConfirm, Label: "是否允许", Required: true, YesLabel: "允许", NoLabel: "不允许"}}
	}
	if request.DedupeKey == "" {
		request.DedupeKey = request.Source + ":round-api"
	}
	var out workbench.InteractionRequest
	if e := a.store.Update(func(st *workbench.State) error {
		created, _, err := workbench.PublishInteraction(st, request)
		out = created
		return err
	}); e != nil {
		a.t.Fatal(e)
	}
	return out
}

func decodeEnvelope(t *testing.T, body []byte) (workbench.InteractionReceipt, map[string]any) {
	t.Helper()
	var envelope workbench.InteractionReceipt
	if e := json.Unmarshal(body, &envelope); e != nil {
		t.Fatalf("envelope %s: %v", body, e)
	}
	var raw map[string]any
	if e := json.Unmarshal(body, &raw); e != nil {
		t.Fatal(e)
	}
	return envelope, raw
}

func TestInteractionAnswerAPIEnvelopeReplayAndRetry(t *testing.T) {
	commander := &interactionCommander{}
	api := setupInteractionAPI(t, commander)
	req := api.seed(workbench.InteractionRequest{})
	body := `{"operationId":"api-answer","revision":0,"type":"answer_interaction","projectId":"` + api.project + `","conversationId":"` + api.conv + `","taskId":"` + api.task + `","requestId":"` + req.ID + `","requestVersion":1,"answerMode":"form","answers":{"allow":false}}`
	w := api.call("POST", "/api/commands", body)
	if w.Code != 200 {
		t.Fatalf("answer %d %s", w.Code, w.Body.String())
	}
	envelope, raw := decodeEnvelope(t, w.Body.Bytes())
	if envelope.InteractionID != req.ID || envelope.CreatedID == "" || envelope.Delivery != workbench.DeliveryRecorded {
		t.Fatalf("envelope %+v", envelope)
	}
	if envelope.Interaction == nil || envelope.Interaction.Status != workbench.InteractionAnswered {
		t.Fatalf("interaction not returned: %s", w.Body.String())
	}
	if raw["type"] != "answer_interaction" || raw["interactionId"] != req.ID {
		t.Fatalf("flat receipt fields missing: %s", w.Body.String())
	}
	if sends, _ := commander.counts(); sends != 1 {
		t.Fatalf("dispatch count %d", sends)
	}
	// Replaying the same operation returns the original receipt without a
	// second dispatch or a duplicate message.
	w = api.call("POST", "/api/commands", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	replay, _ := decodeEnvelope(t, w.Body.Bytes())
	if replay.CreatedID != envelope.CreatedID {
		t.Fatal("replay changed the answer message")
	}
	if sends, _ := commander.counts(); sends != 1 {
		t.Fatalf("replay dispatched again: %d", sends)
	}
	if len(api.store.Snapshot().Conversations[api.conv].Messages) != 1 {
		t.Fatal("replay duplicated the answer message")
	}
	// Only a provably unsent answer may be retried, reusing its message id.
	if e := api.store.Update(func(st *workbench.State) error {
		c := st.Conversations[api.conv]
		c.RunState = "failed"
		c.Messages[0].Delivery = "not_sent"
		c.Interactions[0].Response.Delivery = workbench.DeliveryNotSent
		st.Conversations[api.conv] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	w = api.call("POST", "/api/commands", `{"operationId":"api-retry","projectId":"`+api.project+`","conversationId":"`+api.conv+`","taskId":"`+api.task+`","type":"retry_interaction_answer","requestId":"`+req.ID+`","requestVersion":1}`)
	if w.Code != 200 {
		t.Fatalf("retry %d %s", w.Code, w.Body.String())
	}
	retried, _ := decodeEnvelope(t, w.Body.Bytes())
	if retried.CreatedID != envelope.CreatedID || retried.Delivery != workbench.DeliveryRecorded {
		t.Fatalf("retry envelope %+v", retried)
	}
	if sends, _ := commander.counts(); sends != 2 {
		t.Fatalf("retry did not dispatch once: %d", sends)
	}
	// Uncertain delivery is never retried automatically.
	_ = api.store.Update(func(st *workbench.State) error {
		c := st.Conversations[api.conv]
		c.RunState = "interrupted"
		c.Interactions[0].Response.Delivery = workbench.DeliveryUncertain
		st.Conversations[api.conv] = c
		return nil
	})
	w = api.call("POST", "/api/commands", `{"operationId":"api-retry-2","projectId":"`+api.project+`","conversationId":"`+api.conv+`","taskId":"`+api.task+`","type":"retry_interaction_answer","requestId":"`+req.ID+`","requestVersion":1}`)
	if w.Code != 409 {
		t.Fatalf("uncertain retry %d %s", w.Code, w.Body.String())
	}
}

func TestInteractionAnswerAPIRecordsWhenSessionCannotStart(t *testing.T) {
	for name, kind := range map[string]string{"missing-agent": "missing", "send-error": "error"} {
		t.Run(name, func(t *testing.T) {
			var commander Commander
			if kind == "error" {
				commander = &interactionCommander{sendErr: &workbench.Error{Code: "conflict", Message: "fixture"}}
			}
			api := setupInteractionAPI(t, commander)
			req := api.seed(workbench.InteractionRequest{ID: "ix_fail"})
			w := api.call("POST", "/api/commands", `{"operationId":"api-fail","type":"answer_interaction","projectId":"`+api.project+`","conversationId":"`+api.conv+`","taskId":"`+api.task+`","requestId":"`+req.ID+`","requestVersion":1,"answerMode":"form","answers":{"allow":true}}`)
			if w.Code != 200 {
				t.Fatalf("record %d %s", w.Code, w.Body.String())
			}
			envelope, _ := decodeEnvelope(t, w.Body.Bytes())
			if envelope.Delivery != workbench.DeliveryNotSent || envelope.Notice == "" {
				t.Fatalf("envelope %+v", envelope)
			}
			if envelope.Interaction == nil || envelope.Interaction.Status != workbench.InteractionAnswered || envelope.Interaction.Response.Note == "" {
				t.Fatalf("answer lost: %+v", envelope.Interaction)
			}
			st := api.store.Snapshot()
			c := st.Conversations[api.conv]
			if c.RunState != "failed" || c.Error == "" {
				t.Fatalf("conversation not marked unsent: %+v", c)
			}
			for _, msg := range c.Messages {
				if msg.Role == "user" && msg.Delivery != "not_sent" {
					t.Fatalf("message delivery %q", msg.Delivery)
				}
			}
			if len(c.Interactions) != 1 {
				t.Fatal("answer request missing")
			}
		})
	}
}

func TestInteractionAPIRejectsInvalidPayloadAndForeignScope(t *testing.T) {
	commander := &interactionCommander{}
	api := setupInteractionAPI(t, commander)
	req := api.seed(workbench.InteractionRequest{ID: "ix_scope"})
	base := `"type":"answer_interaction","projectId":"` + api.project + `","conversationId":"` + api.conv + `","requestId":"` + req.ID + `","requestVersion":1,"answers":{"allow":true}`
	for name, body := range map[string]string{
		"no-operation":    `{` + strings.TrimPrefix(base, `"type"`) + `}`,
		"no-request":      `{"operationId":"x-1","type":"answer_interaction","projectId":"` + api.project + `","conversationId":"` + api.conv + `","requestVersion":1,"answers":{"allow":true}}`,
		"no-version":      `{"operationId":"x-2","type":"answer_interaction","projectId":"` + api.project + `","conversationId":"` + api.conv + `","requestId":"` + req.ID + `","answers":{"allow":true}}`,
		"unknown-answer":  `{"operationId":"x-3",` + base + `,"answers":{"admin":true}}`,
		"foreign-project": `{"operationId":"x-4","type":"answer_interaction","projectId":"other","conversationId":"` + api.conv + `","requestId":"` + req.ID + `","requestVersion":1,"answers":{"allow":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := api.call("POST", "/api/commands", body)
			if w.Code == 200 {
				t.Fatalf("invalid payload accepted: %s", w.Body.String())
			}
		})
	}
	if sends, _ := commander.counts(); sends != 0 {
		t.Fatal("invalid payload dispatched")
	}
	if len(api.store.Snapshot().Conversations[api.conv].Messages) != 0 {
		t.Fatal("invalid payload created a message")
	}
}

func TestInteractionNativeCancelDispatchesUpstreamOnly(t *testing.T) {
	commander := &interactionCommander{}
	api := setupInteractionAPI(t, commander)
	req := api.seed(workbench.InteractionRequest{ID: "ix_native", Source: workbench.SourceAgentUI, Native: &workbench.InteractionNative{Method: "confirm", UpstreamID: "upstream-api", RunID: "run_api1234", Generation: 1}})
	w := api.call("POST", "/api/commands", `{"operationId":"api-cancel","type":"cancel_interaction","projectId":"`+api.project+`","conversationId":"`+api.conv+`","taskId":"`+api.task+`","requestId":"`+req.ID+`","requestVersion":1}`)
	if w.Code != 200 {
		t.Fatalf("cancel %d %s", w.Code, w.Body.String())
	}
	envelope, _ := decodeEnvelope(t, w.Body.Bytes())
	if envelope.Interaction == nil || envelope.Interaction.Status != workbench.InteractionCancelled || envelope.Delivery != workbench.DeliveryRecorded {
		t.Fatalf("cancel envelope %+v", envelope)
	}
	sends, answers := commander.counts()
	if sends != 0 || answers != 1 {
		t.Fatalf("native cancel used the wrong dispatch path: sends=%d answers=%d", sends, answers)
	}
	if len(api.store.Snapshot().Conversations[api.conv].Messages) != 0 {
		t.Fatal("native cancel created a chat message")
	}
	// A snapshot advertises the interaction capability for feature detection.
	w = api.call("GET", "/api/snapshot", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"interactions":true`) {
		t.Fatalf("capability missing: %d %s", w.Code, w.Body.String())
	}
}

func TestInteractionBatchDispatchWaitsForAllAnswers(t *testing.T) {
	commander := &interactionCommander{}
	api := setupInteractionAPI(t, commander)
	first := api.seed(workbench.InteractionRequest{ID: "ix_batch_api1", RoundID: "r1", DedupeKey: "assistant_question:r1"})
	second := api.seed(workbench.InteractionRequest{ID: "ix_batch_api2", RoundID: "r2", DedupeKey: "assistant_question:r2"})
	answer := func(op, id string) *httptest.ResponseRecorder {
		return api.call("POST", "/api/commands", `{"operationId":"`+op+`","type":"answer_interaction","projectId":"`+api.project+`","conversationId":"`+api.conv+`","taskId":"`+api.task+`","requestId":"`+id+`","requestVersion":1,"answers":{"allow":true}}`)
	}
	w := answer("batch-api-1", first.ID)
	if w.Code != 200 {
		t.Fatalf("first %d %s", w.Code, w.Body.String())
	}
	envelope, _ := decodeEnvelope(t, w.Body.Bytes())
	if envelope.CreatedID != "" || envelope.Interaction == nil || envelope.Interaction.Response.Note == "" {
		t.Fatalf("first answer dispatched early: %+v", envelope)
	}
	if sends, _ := commander.counts(); sends != 0 {
		t.Fatalf("dispatched a partial batch: %d", sends)
	}
	if len(api.store.Snapshot().Conversations[api.conv].Messages) != 0 {
		t.Fatal("partial batch created a message")
	}
	w = answer("batch-api-2", second.ID)
	if w.Code != 200 {
		t.Fatalf("second %d %s", w.Code, w.Body.String())
	}
	envelope, _ = decodeEnvelope(t, w.Body.Bytes())
	if envelope.CreatedID == "" || envelope.Delivery != workbench.DeliveryRecorded {
		t.Fatalf("last answer envelope %+v", envelope)
	}
	if sends, _ := commander.counts(); sends != 1 {
		t.Fatalf("expected exactly one aggregated dispatch, got %d", sends)
	}
	c := api.store.Snapshot().Conversations[api.conv]
	if len(c.Messages) != 1 || c.Messages[0].ID != envelope.CreatedID {
		t.Fatalf("aggregated message %+v", c.Messages)
	}
	for _, r := range c.Interactions {
		if r.Response == nil || r.Response.MessageID != envelope.CreatedID || r.Response.FollowUpRoundID != envelope.CreatedID {
			t.Fatalf("batch link %+v", r.Response)
		}
	}
}
