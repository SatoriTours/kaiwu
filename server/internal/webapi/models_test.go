package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type modelFixture struct{ calls int }

func (m *modelFixture) List(context.Context) ([]pimodel.Model, error) {
	m.calls++
	return []pimodel.Model{{Provider: "fixture", ID: "test"}}, nil
}
func TestModelsAuthenticationAndSelectionValidation(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	models := &modelFixture{}
	h := New(Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331", Models: models}, s, nil).(*handler)
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		r.Header.Set("X-Kaiwu-Client", "web")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/models", "", nil); w.Code != 401 || models.calls != 0 {
		t.Fatal("unauthenticated discovery")
	}
	w := call("POST", "/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil)
	cookie := w.Result().Cookies()[0]
	w = call("GET", "/api/models", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"test"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		model, thinking string
		status          int
	}{{"fake", "", 400}, {"test", "high", 400}, {"test", "", 200}} {
		before := s.Snapshot().Revision
		b, _ := json.Marshal(workbench.Command{OperationID: "model-" + tc.model + tc.thinking, Revision: before, Type: "set_default_profile", Provider: "fixture", Model: tc.model, Thinking: tc.thinking})
		w = call("POST", "/api/commands", string(b), cookie)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status != 200 && s.Snapshot().Revision != before {
			t.Fatal("rejected model saved")
		}
	}
	if s.Snapshot().Profile.CredentialSource != "server_pi" {
		t.Fatal("credential source missing")
	}
}
