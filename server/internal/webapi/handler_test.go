package webapi

import (
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthenticationHostOriginAndCommands(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "s.db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	token := strings.Repeat("x", 32)
	h := New(Config{Token: token, Host: "127.0.0.1:7331"}, s, nil)
	call := func(method, path, body, origin, host string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kaiwu-Client", "web")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/snapshot", "", "", "127.0.0.1:7331", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/login", `{"token":"bad"}`, "http://127.0.0.1:7331", "127.0.0.1:7331", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/api/login", `{"token":"`+token+`"}`, "http://127.0.0.1:7331", "127.0.0.1:7331", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal(cookies)
	}
	cookie := cookies[0]
	if w = call("GET", "/api/snapshot", "", "", "127.0.0.1:7331", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, host := range []string{"evil.test:7331", "127.0.0.1:6666"} {
		if w = call("GET", "/api/snapshot", "", "", host, cookie); w.Code != 403 {
			t.Fatal("host", w.Code)
		}
	}
	if w = call("POST", "/api/commands", `{}`, "http://evil.test", "127.0.0.1:7331", cookie); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	if w = call("POST", "/api/commands", `{"operationId":"test-key","revision":1,"type":"add_topic","projectId":"other"}`, "http://127.0.0.1:7331", "127.0.0.1:7331", cookie); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/api/unknown", "", "", "127.0.0.1:7331", cookie); w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatal("unknown API", w.Code)
	}
	h.(*handler).cfg.AgentAvailable = true
	receipt, e := s.Command(workbench.Command{OperationID: "profile-project", Revision: s.Snapshot().Revision, Type: "add_project", Name: "no profile", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot().Projects[receipt.CreatedID]
	w = call("POST", "/api/commands", `{"operationId":"profile-send","revision":0,"type":"send_message","projectId":"`+p.ID+`","conversationId":"`+p.ConversationID+`","text":"hello"}`, "http://127.0.0.1:7331", "127.0.0.1:7331", cookie)
	if w.Code != 503 || len(s.Snapshot().Conversations[p.ConversationID].Messages) != 0 {
		t.Fatal("unconfigured frozen profile started", w.Code)
	}

}
