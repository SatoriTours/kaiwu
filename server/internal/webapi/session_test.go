package webapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestRememberedLoginSurvivesRestartAndLogoutRevokesIt(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state.db")
	token := strings.Repeat("a", 32)
	var store *workbench.Store
	var h http.Handler
	start := func(token string) {
		var err error
		store, err = workbench.Open(db, workbench.Profile{})
		if err != nil {
			t.Fatal(err)
		}
		h = New(Config{Token: token, Host: "127.0.0.1:7331"}, store, nil)
	}
	start(token)
	t.Cleanup(func() { store.Close() })
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		r.Header.Set("X-Kaiwu-Client", "web")
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := func() *http.Cookie {
		w := call("POST", "/api/login", `{"token":"`+token+`"}`, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		c := w.Result().Cookies()[0]
		if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
			t.Fatal("unsafe cookie")
		}
		return c
	}
	cookie := login()
	if w := call("GET", "/api/snapshot", "", cookie); w.Code != 200 {
		t.Fatal("refresh lost login", w.Code)
	}
	store.Close()
	start(token)
	if w := call("GET", "/api/snapshot", "", cookie); w.Code != 200 {
		t.Fatal("restart lost login", w.Code)
	}
	if cookie.MaxAge < 29*24*3600 || time.Until(cookie.Expires) < 29*24*time.Hour {
		t.Fatal("login was not remembered for 30 days")
	}
	tampered := *cookie
	tampered.Value += "x"
	if w := call("GET", "/api/snapshot", "", &tampered); w.Code != 401 {
		t.Fatal("tampered cookie accepted")
	}
	if w := call("POST", "/api/logout", `{}`, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	store.Close()
	start(token)
	if w := call("GET", "/api/snapshot", "", cookie); w.Code != 401 {
		t.Fatal("logout did not revoke persistent login")
	}
	cookie = login()
	store.Close()
	start(strings.Repeat("b", 32))
	if w := call("GET", "/api/snapshot", "", cookie); w.Code != 401 {
		t.Fatal("token rotation must revoke login")
	}
}
