package webapi

import (
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIConfigVersionedWritesNeverExposeOrAcceptCredentialBodies(t *testing.T) {
	s, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConfigureAI(filepath.Join(t.TempDir(), "config")); err != nil {
		t.Fatal(err)
	}
	h := New(Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331"}, s, nil)
	defer h.(*handler).Close()
	var cookie *http.Cookie
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(string(b)))
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kaiwu-Client", "web")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := request("POST", "/api/login", map[string]any{"token": strings.Repeat("x", 32)})
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	cookie = login.Result().Cookies()[0]
	first := request("GET", "/api/ai/config", nil)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body)
	}
	var view struct {
		Version string `json:"version"`
	}
	json.Unmarshal(first.Body.Bytes(), &view)
	body := map[string]any{"version": view.Version, "config": map[string]any{"schemaVersion": 1, "rules": []string{"test rule"}}}
	response := request("PUT", "/api/ai/config", body)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body)
	}
	stale := request("PUT", "/api/ai/config", body)
	if stale.Code != 409 {
		t.Fatal("stale write accepted", stale.Code, stale.Body)
	}
	bad := request("PUT", "/api/ai/config", map[string]any{"config": map[string]any{"schemaVersion": 1, "apiKey": "synthetic-secret"}})
	if bad.Code != 400 || strings.Contains(bad.Body.String(), "synthetic-secret") {
		t.Fatal("credential accepted or reflected", bad.Code, bad.Body)
	}
}
