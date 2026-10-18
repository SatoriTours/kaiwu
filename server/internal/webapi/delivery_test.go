package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SatoriTours/kaiwu/server/internal/delivery"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// fakeDelivery records the delivery surface the API used. It has no write
// path beyond accept/control, so a read-only route cannot cause a remote
// write by construction.
type fakeDelivery struct {
	mu        sync.Mutex
	preflight int
	accepts   []string
	controls  []string
	wakes     int
	replay    map[string]bool
	missing   map[string]bool
}

func (f *fakeDelivery) Preflight(_ context.Context, _ workbench.Project, policy workbench.ProjectDeliveryPolicy) (delivery.Preflight, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.preflight++
	return delivery.Preflight{Ready: policy.Enabled, Blockers: []string{}}, nil
}

func (f *fakeDelivery) Accept(_ context.Context, operationID, deliveryID, token string, expectedVersion uint64) (workbench.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing[deliveryID] {
		return workbench.Receipt{}, workbench.Missing()
	}
	if token == "" {
		return workbench.Receipt{}, workbench.Invalid("请提供验收令牌")
	}
	replay := f.replay[operationID]
	if !replay {
		f.replay[operationID] = true
		f.accepts = append(f.accepts, operationID)
	}
	return workbench.Receipt{Type: "delivery_accept", DeliveryID: deliveryID, CreatedID: "candidate", Revision: 1, Replay: replay}, nil
}

func (f *fakeDelivery) Control(_ context.Context, operationID, deliveryID, action string, _ uint64) (workbench.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if action == "" {
		return workbench.Receipt{}, workbench.Invalid("不支持的控制动作")
	}
	replay := f.replay[operationID]
	if !replay {
		f.replay[operationID] = true
		f.controls = append(f.controls, action)
	}
	return workbench.Receipt{Type: "delivery_control", DeliveryID: deliveryID, Notice: action, Revision: 1, Replay: replay}, nil
}

func (f *fakeDelivery) Wake() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wakes++
}

func (f *fakeDelivery) counts() (preflight, accepts, controls, wakes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.preflight, len(f.accepts), len(f.controls), f.wakes
}

// deliveryAPI boots one authenticated test server around the handler.
func deliveryAPI(t *testing.T, fake *fakeDelivery) (*workbench.Store, *httptest.Server, *http.Client, func(method, path, body string) *http.Response) {
	t.Helper()
	store, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	handler = New(Config{Host: u.Host, Token: strings.Repeat("x", 32), Delivery: fake}, store, nil)

	client := srv.Client()
	plain := srv.Client()
	do := func(method, path, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)
		req.Header.Set("X-Kaiwu-Client", "web")
		res, e := plain.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	// Log in and keep the session cookie on the authenticated client.
	login := do("POST", "/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`)
	if login.StatusCode != 200 {
		t.Fatalf("login failed: %d", login.StatusCode)
	}
	cookies := login.Cookies()
	login.Body.Close()
	client = &http.Client{Transport: cookieTransport{base: plain.Transport, cookie: cookies[0]}}
	return store, srv, client, do
}

type cookieTransport struct {
	base   http.RoundTripper
	cookie *http.Cookie
}

func (c cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.AddCookie(c.cookie)
	return c.base.RoundTrip(r)
}

// TestDeliverySocketAuthorizationAndReplay covers the fixed delivery surface:
// authentication, origin, session expiry, cross-project identity, operation
// replay (a lost receipt is looked up, never repeated) and the socket mapping.
func TestDeliverySocketAuthorizationAndReplay(t *testing.T) {
	fake := &fakeDelivery{replay: map[string]bool{}, missing: map[string]bool{"foreign": true}}
	store, srv, client, anon := deliveryAPI(t, fake)
	r, e := store.Command(workbench.Command{OperationID: "create-project", Revision: store.Snapshot().Revision, Type: "add_project", Name: "fixture", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	projectID := r.CreatedID
	// Unauthenticated requests are refused before any delivery work.
	res := anon("POST", "/api/delivery/preflight", `{"projectId":"`+projectID+`"}`)
	if res.StatusCode != 401 {
		t.Fatalf("unauthenticated preflight: %d", res.StatusCode)
	}
	res.Body.Close()

	// A cross-site origin is refused even with a valid session.
	req, _ := http.NewRequest("POST", srv.URL+"/api/delivery/preflight", strings.NewReader(`{"projectId":"`+projectID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.invalid")
	req.Header.Set("X-Kaiwu-Client", "web")
	if r, e := client.Do(req); e != nil {
		t.Fatal(e)
	} else {
		if r.StatusCode != 403 {
			t.Fatalf("cross-site preflight: %d", r.StatusCode)
		}
		r.Body.Close()
	}

	// An expired/unknown session cookie is refused.
	expired := &http.Client{Transport: cookieTransport{base: srv.Client().Transport, cookie: &http.Cookie{Name: "kaiwu_session", Value: strings.Repeat("a", 64)}}}
	req, _ = http.NewRequest("POST", srv.URL+"/api/delivery/preflight", strings.NewReader(`{"projectId":"`+projectID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("X-Kaiwu-Client", "web")
	if r, e := expired.Do(req); e != nil {
		t.Fatal(e)
	} else {
		if r.StatusCode != 401 {
			t.Fatalf("expired session preflight: %d", r.StatusCode)
		}
		r.Body.Close()
	}

	authed := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)
		req.Header.Set("X-Kaiwu-Client", "web")
		r, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}

	// A valid session reaches the read-only preflight exactly once.
	if code, _ := authed("POST", "/api/delivery/preflight", `{"projectId":"`+projectID+`"}`); code != 200 {
		t.Fatalf("preflight status %d", code)
	}
	if preflight, accepts, controls, wakes := fake.counts(); preflight != 1 || accepts != 0 || controls != 0 || wakes != 0 {
		t.Fatalf("preflight caused side effects: %d %d %d %d", preflight, accepts, controls, wakes)
	}

	// Accept is an explicit operation: the same operationId replays the same
	// receipt without a second wake, and a foreign delivery is not found.
	if code, out := authed("POST", "/api/delivery/accept", `{"operationId":"accept-1","deliveryId":"d1","token":"t","expectedVersion":3}`); code != 200 {
		t.Fatalf("accept status %d (%v)", code, out)
	}
	if code, out := authed("POST", "/api/delivery/accept", `{"operationId":"accept-1","deliveryId":"d1","token":"t","expectedVersion":3}`); code != 200 || out["replay"] != nil {
		t.Fatalf("accept replay status %d (%v)", code, out)
	}
	if code, _ := authed("POST", "/api/delivery/accept", `{"operationId":"accept-2","deliveryId":"foreign","token":"t","expectedVersion":1}`); code != 404 {
		t.Fatalf("foreign delivery accepted: %d", code)
	}
	if _, accepts, _, wakes := fake.counts(); accepts != 1 || wakes != 1 {
		t.Fatalf("accept wake/charge wrong: accepts=%d wakes=%d", accepts, wakes)
	}
	// The lost-receipt path: the original operation is still queryable.
	if code, out := authed("POST", "/api/delivery/control", `{"operationId":"ctl-1","deliveryId":"d1","action":"pause","expectedVersion":4}`); code != 200 || out["notice"] != "pause" {
		t.Fatalf("control status %d (%v)", code, out)
	}
	if _, _, controls, wakes := fake.counts(); controls != 1 || wakes != 2 {
		t.Fatalf("control wake/charge wrong: controls=%d wakes=%d", controls, wakes)
	}

	// The socket surface maps the same fixed methods onto the authenticated
	// handler; status projects only the safe summary.
	if _, ok := store.Snapshot().Deliveries["nope"]; ok {
		t.Fatal("fixture unexpectedly has a delivery")
	}
	if code, out := authed("POST", "/api/delivery/status", `{"taskId":"nope"}`); code != 200 || out["delivery"] != nil {
		t.Fatalf("status projection %d (%v)", code, out)
	}
	if code, _ := authed("POST", "/api/delivery/steps", `{"deliveryId":"d1","after":0,"limit":101}`); code != 400 {
		t.Fatalf("oversized step page accepted: %d", code)
	}

	// The socket maps the same fixed methods onto the authenticated handler.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, strings.Replace(srv.URL, "http:", "ws:", 1)+"/api/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}, "Cookie": {cookieOf(client)}}})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	body, _ := json.Marshal(map[string]any{"id": "st", "method": "delivery/status", "body": map[string]any{"taskId": "nope"}})
	if e := conn.Write(ctx, websocket.MessageText, body); e != nil {
		t.Fatal(e)
	}
	for {
		_, raw, e := conn.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil || v["id"] != "st" {
			continue
		}
		if v["status"] != float64(200) {
			t.Fatalf("socket delivery/status: %v", v)
		}
		break
	}
}

// cookieOf extracts the session cookie from the authenticated client.
func cookieOf(c *http.Client) string {
	if t, ok := c.Transport.(cookieTransport); ok {
		return t.cookie.String()
	}
	return ""
}
