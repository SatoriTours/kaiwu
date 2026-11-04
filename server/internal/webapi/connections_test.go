package webapi

import (
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiConnectionsAuthenticatedCRUDAndSocket(t *testing.T) {
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cs, e := pimodel.OpenConnections(filepath.Join(root, "private"))
	if e != nil {
		t.Fatal(e)
	}
	h := New(Config{DataDir: root, Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331", Connections: cs}, s, nil).(*handler)
	defer h.Close()
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331/api/"+path, strings.NewReader(body))
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
	body := `{"name":"my","api":"openai-completions","baseUrl":"https://example.com/v1","key":"never-in-response","models":["one"]}`
	if w := call("POST", "pi/connections/save", body, nil); w.Code != 401 || len(cs.List()) != 0 {
		t.Fatal("auth bypass")
	}
	login := call("POST", "login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil)
	cookie := login.Result().Cookies()[0]
	before := s.Snapshot().Revision
	w := call("POST", "pi/connections/save", body, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "never-in-response") {
		t.Fatal(w.Code, w.Body.String())
	}
	list := call("GET", "pi/connections", "", cookie)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"name":"my"`) {
		t.Fatal(list.Code)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7331/api/socket", nil)
	r.Header.Set("Origin", "http://127.0.0.1:7331")
	r.AddCookie(cookie)
	reply := h.socketCall(r, socketRequest{ID: "list", Method: "pi/connections"})
	if reply.Status != 200 || strings.Contains(string(reply.Data), "never-in-response") {
		t.Fatal(reply.Status)
	}
	if s.Snapshot().Revision != before {
		t.Fatal("secrets must not enter workbench snapshot")
	}
	c := cs.List()[0]
	b, _ := json.Marshal(map[string]any{"id": c.ID, "revision": c.Revision})
	if w = call("POST", "pi/connections/remove", string(b), cookie); w.Code != 200 || len(cs.List()) != 0 {
		t.Fatal("remove", w.Code)
	}
	if w = call("POST", "pi/connections/remove", string(b), cookie); w.Code != 409 {
		t.Fatal("stale remove", w.Code)
	}
	oauth := call("POST", "pi/oauth/start", `{"name":"Claude","provider":"anthropic"}`, cookie)
	if oauth.Code != 200 || !strings.Contains(oauth.Body.String(), "claude.ai") {
		t.Fatal("OAuth start", oauth.Code)
	}
}
