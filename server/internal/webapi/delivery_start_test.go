package webapi

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// An enabled task must have one owner of dispatch, not a race between the
// HTTP handler and the delivery worker. An unavailable worker must not start
// an unmonitored development cycle.
func TestDeliveryStartDispatchOwnedByWorker(t *testing.T) {
	for _, tc := range []struct {
		name            string
		available, chat bool
	}{
		{"worker", true, false}, {"unavailable", false, false}, {"chat-worker", true, true}, {"chat-unavailable", false, true},
	} {
		available := tc.available
		t.Run(tc.name, func(t *testing.T) {
			s, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "test", Model: "test"})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			command := func(c workbench.Command) workbench.Receipt {
				t.Helper()
				c.Revision = s.Snapshot().Revision
				r, e := s.Command(c)
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
			task := command(workbench.Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "test"}).CreatedID
			policy := workbench.DefaultDeliveryPolicy(p)
			policy.Enabled = true
			policy.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
			policy.Checks = []workbench.DeliveryCheck{{ID: "check", Argv: []string{"true"}, Dir: ".", TimeoutSeconds: 300}}
			policy.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1}}
			policy.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2}}
			policy.GitCredentialRef = "server_github_https"
			policy.APICredentialRef = "server_github_api"
			policy.Actor = "local-login"
			policy.AuthorizationDigest = "explicit-scope"
			policy.AuthorizedAt = time.Now().UTC()
			if _, err = s.SaveDeliveryPolicy("enable", 0, policy); err != nil {
				t.Fatal(err)
			}
			f := &fakeDelivery{replay: map[string]bool{}}
			controller := &startCommander{}
			cfg := Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331", AgentAvailable: true}
			if available {
				cfg.Delivery = f
			}
			h := New(cfg, s, controller).(*handler)
			defer h.Close()
			login := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/login", strings.NewReader(`{"token":"`+cfg.Token+`"}`))
			login.Header.Set("Content-Type", "application/json")
			logged := httptest.NewRecorder()
			h.login(logged, login)
			c := workbench.Command{OperationID: "start", Type: "start_task", ProjectID: p, TaskID: task, Revision: s.Snapshot().Revision}
			if tc.chat {
				c.Type = "send_message"
				c.TaskID = ""
				c.ConversationID = s.Snapshot().Tasks[task].ConversationID
				c.Text = "开始开发"
			}
			body, _ := json.Marshal(c)
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/commands", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "http://127.0.0.1:7331")
				req.Header.Set("X-Kaiwu-Client", "web")
				req.AddCookie(logged.Result().Cookies()[0])
				res := httptest.NewRecorder()
				h.ServeHTTP(res, req)
				want := 200
				if !available {
					want = 503
				}
				if res.Code != want {
					t.Fatalf("status=%d want=%d: %s", res.Code, want, res.Body.String())
				}
			}
			if controller.sends != 0 {
				t.Fatalf("handler bypassed delivery worker: %d direct sends", controller.sends)
			}
			_, _, _, wakes := f.counts()
			if available && wakes != 1 {
				t.Fatalf("start/replay must wake worker once, got %d", wakes)
			}
			if !available && len(s.Snapshot().Deliveries) != 0 {
				t.Fatal("unavailable worker admitted a cycle")
			}
		})
	}
}
