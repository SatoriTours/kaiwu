package webapi

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventConnectionClosesWithOwnerNotClient(t *testing.T) {
	store, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := Config{Context: owner, Token: strings.Repeat("x", 32)}
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	cfg.Host = u.Host
	handler = New(cfg, store, nil)
	request, _ := http.NewRequest("POST", server.URL+"/api/login", strings.NewReader(`{"token":"`+cfg.Token+`"}`))
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kaiwu-Client", "web")
	resp, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	headers := http.Header{"Origin": {server.URL}, "Cookie": {resp.Cookies()[0].String()}}
	timeout, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	conn, _, e := websocket.Dial(timeout, strings.Replace(server.URL, "http:", "ws:", 1)+"/api/events", &websocket.DialOptions{HTTPHeader: headers})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	if _, _, e = conn.Read(timeout); e != nil {
		t.Fatal(e)
	}
	cancel()
	if _, _, e = conn.Read(timeout); e == nil || timeout.Err() != nil {
		t.Fatalf("owner did not close websocket promptly: %v", e)
	}
}
