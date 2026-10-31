package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/config"
	"github.com/SatoriTours/kaiwu/server/internal/httpapi"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func localClient(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

type runningServer struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // Written before done closes; only read after that synchronization.
}

func startServer(t *testing.T, srv *http.Server, ln net.Listener, timeout time.Duration) *runningServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &runningServer{cancel: cancel, done: make(chan struct{})}
	go func() {
		r.err = serve(ctx, srv, ln, timeout, discardLogger())
		close(r.done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			t.Error("server did not exit during cleanup")
		}
	})
	return r
}

func waitDone(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}
}

func assertPortClosed(t *testing.T, addr string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener remained open")
	}
}

func TestServerDefaults(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second ||
		srv.WriteTimeout != 15*time.Second || srv.IdleTimeout != 60*time.Second || srv.MaxHeaderBytes != 1<<20 {
		t.Fatalf("incorrect HTTP server limits: %+v", srv)
	}
}

func TestServeHealthAndCancel(t *testing.T) {
	ln := loopbackListener(t)
	r := startServer(t, newServer(httpapi.NewHandler(discardLogger())), ln, time.Second)
	resp, err := localClient(t).Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || readErr != nil || string(body) != "{\"status\":\"ok\",\"service\":\"kaiwu-server\"}\n" {
		t.Fatalf("response=%d %q err=%v", resp.StatusCode, body, readErr)
	}
	r.cancel()
	waitDone(t, r.done)
	if r.err != nil {
		t.Fatal(r.err)
	}
	assertPortClosed(t, ln.Addr().String())
}

func TestRunRejectsUnsafeConfig(t *testing.T) {
	var logs bytes.Buffer
	err := Run(context.Background(), config.Config{ListenAddr: "0.0.0.0:7331"}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err == nil || strings.Contains(logs.String(), "server started") {
		t.Fatalf("error=%v logs=%s", err, logs.String())
	}
}

func TestRunCanceledBeforeStart(t *testing.T) {
	ln := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var logs bytes.Buffer
	if err := Run(ctx, config.Config{ListenAddr: ln.Addr().String()}, slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "server started") {
		t.Fatal("canceled run started server")
	}
}

func TestRunPortInUse(t *testing.T) {
	ln := loopbackListener(t)
	var logs bytes.Buffer
	err := Run(context.Background(), config.Config{ListenAddr: ln.Addr().String()}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err == nil || strings.Contains(logs.String(), "server started") {
		t.Fatalf("error=%v logs=%s", err, logs.String())
	}
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("original listener damaged: %v", err)
	}
	_ = conn.Close()
}

func TestServeGracefulInflight(t *testing.T) {
	started, release, shutdown := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		select {
		case <-release:
			_, _ = io.WriteString(w, "finished")
		case <-req.Context().Done():
		}
	})
	srv := newServer(h)
	srv.RegisterOnShutdown(func() { close(shutdown) })
	ln := loopbackListener(t)
	r := startServer(t, srv, ln, time.Second)
	client := localClient(t)
	result := make(chan string, 1)
	go func() {
		resp, err := client.Get("http://" + ln.Addr().String())
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- string(body)
	}()
	waitDone(t, started)
	r.cancel()
	waitDone(t, shutdown)
	select {
	case got := <-result:
		t.Fatalf("inflight request interrupted: %s", got)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case got := <-result:
		if got != "finished" {
			t.Fatalf("response=%q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
	waitDone(t, r.done)
	if r.err != nil {
		t.Fatal(r.err)
	}
	assertPortClosed(t, ln.Addr().String())
}

func TestServeShutdownTimeout(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
		close(stopped)
	})
	ln := loopbackListener(t)
	r := startServer(t, newServer(h), ln, 50*time.Millisecond)
	client := localClient(t)
	requestDone := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://" + ln.Addr().String())
		if resp != nil {
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	waitDone(t, started)
	r.cancel()
	waitDone(t, r.done)
	if !errors.Is(r.err, context.DeadlineExceeded) {
		t.Fatalf("error=%v want deadline exceeded", r.err)
	}
	waitDone(t, stopped)
	select {
	case err := <-requestDone:
		if err == nil {
			t.Fatal("request survived forced close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not exit")
	}
	assertPortClosed(t, ln.Addr().String())
}

// temporaryOnceListener injects one genuine net.Error at the Accept boundary,
// then delegates to a real loopback listener. It avoids exhausting process FDs.
type temporaryOnceListener struct {
	net.Listener
	failed bool // Only the Serve goroutine calls Accept.
}

func (ln *temporaryOnceListener) Accept() (net.Conn, error) {
	if !ln.failed {
		ln.failed = true
		return nil, temporaryAcceptError{}
	}
	return ln.Listener.Accept()
}

type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "injected temporary accept failure" }
func (temporaryAcceptError) Timeout() bool   { return false }
func (temporaryAcceptError) Temporary() bool { return true }

func TestTemporaryAcceptErrorUsesJSONLog(t *testing.T) {
	ln := &temporaryOnceListener{Listener: loopbackListener(t)}
	ctx, cancel := context.WithCancel(context.Background())
	srv := newServer(http.NotFoundHandler())
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	done := make(chan struct{})
	var serveErr error
	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
		waitDone(t, done)
	})
	go func() {
		serveErr = serve(ctx, srv, ln, time.Second, logger)
		close(done)
	}()
	resp, err := localClient(t).Get("http://" + ln.Addr().String() + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("server did not recover from temporary Accept error: %d", resp.StatusCode)
	}
	cancel()
	waitDone(t, done)
	if serveErr != nil {
		t.Fatal(serveErr)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("non-JSON server log: %q", line)
		}
		msg, _ := entry["msg"].(string)
		if strings.Contains(msg, "injected temporary accept failure") {
			found = true
			if entry["level"] != "ERROR" {
				t.Fatalf("server diagnostic level: %v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("temporary Accept error bypassed JSON logger: %s", logs.String())
	}
}

func TestServeUnexpectedExit(t *testing.T) {
	ln := loopbackListener(t)
	r := startServer(t, newServer(http.NotFoundHandler()), ln, time.Second)
	_ = ln.Close()
	waitDone(t, r.done)
	if r.err == nil {
		t.Fatal("unexpected Serve failure was ignored")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitForHealth(t *testing.T, client *http.Client, addr string) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		resp, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("service never became healthy")
}

// postJSON performs one same-origin Web request and returns status and body.
func postJSON(t *testing.T, client *http.Client, raw, cookie, body string) (int, string, []*http.Cookie) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", raw, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	req.Header.Set("X-Kaiwu-Client", "web")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out), resp.Cookies()
}

// TestDeliveryAppLifecycle proves the production assembly: with credentials the
// delivery surface is wired and recovered before traffic; without them the
// basic service still starts and delivery is disabled; credentials never reach
// the snapshot; and shutdown releases the store after the delivery worker.
func TestDeliveryAppLifecycle(t *testing.T) {
	t.Run("credentials wire delivery before traffic", func(t *testing.T) {
		t.Setenv("KAIWU_GITHUB_API_TOKEN", strings.Repeat("a", 40))
		t.Setenv("KAIWU_GITHUB_GIT_TOKEN", strings.Repeat("b", 40))
		dir := t.TempDir()
		addr := freeAddr(t)
		token := strings.Repeat("k", 32)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, config.Config{ListenAddr: addr, Workbench: config.Workbench{Token: token, DataDir: dir}}, discardLogger())
		}()
		client := localClient(t)
		waitForHealth(t, client, addr)

		code, _, cookies := postJSON(t, client, "http://"+addr+"/api/login", "", `{"token":"`+token+`"}`)
		if code != 200 || len(cookies) == 0 {
			t.Fatalf("login failed: %d", code)
		}
		cookie := cookies[0].String()

		// Delivery is assembled: an unknown project is a 404, not a 503
		// "delivery unavailable".
		code, body, _ := postJSON(t, client, "http://"+addr+"/api/delivery/preflight", cookie, `{"projectId":"missing"}`)
		if code != 404 {
			t.Fatalf("delivery preflight not wired: %d %s", code, body)
		}
		// Credential material never reaches the browser snapshot.
		code, body, _ = postJSON(t, client, "http://"+addr+"/api/delivery/status", cookie, `{"taskId":"missing"}`)
		if code != 200 {
			t.Fatalf("status failed: %d %s", code, body)
		}
		snapCode, snap, _ := postJSON(t, client, "http://"+addr+"/api/delivery/policy", cookie, `{}`)
		if snapCode == 200 {
			t.Fatal("policy write without a project unexpectedly succeeded")
		}
		_ = snap
		req, _ := http.NewRequest("GET", "http://"+addr+"/api/snapshot", nil)
		req.Header.Set("Cookie", cookie)
		if resp, err := client.Do(req); err == nil {
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			for _, secret := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40), token} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("credential material leaked into the snapshot")
				}
			}
		}

		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("service did not stop")
		}
		// Delivery (and the agent) closed before the store: the database can
		// be reopened without a lingering worker.
		store, err := workbench.Open(filepath.Join(dir, "workbench.db"), workbench.Profile{})
		if err != nil {
			t.Fatalf("store not released: %v", err)
		}
		_ = store.Close()
	})

	t.Run("without credentials the basic service still starts", func(t *testing.T) {
		t.Setenv("KAIWU_GITHUB_API_TOKEN", "")
		t.Setenv("KAIWU_GITHUB_GIT_TOKEN", "")
		t.Setenv("KAIWU_GITHUB_SSH_KEY", "")
		t.Setenv("KAIWU_GITHUB_SSH_AGENT", "")
		t.Setenv("SSH_AUTH_SOCK", "")
		dir := t.TempDir()
		addr := freeAddr(t)
		token := strings.Repeat("m", 32)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, config.Config{ListenAddr: addr, Workbench: config.Workbench{Token: token, DataDir: dir}}, discardLogger())
		}()
		client := localClient(t)
		waitForHealth(t, client, addr)
		code, _, cookies := postJSON(t, client, "http://"+addr+"/api/login", "", `{"token":"`+token+`"}`)
		if code != 200 || len(cookies) == 0 {
			t.Fatalf("login failed: %d", code)
		}
		// Disabled delivery answers 503 and never pretends to be ready.
		code, _, _ = postJSON(t, client, "http://"+addr+"/api/delivery/preflight", cookies[0].String(), `{"projectId":"missing"}`)
		if code != 503 {
			t.Fatalf("disabled delivery answered %d", code)
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("service did not stop")
		}
	})
}

// TestVersionEndpointAppLifecycle proves the production assembly exposes the
// authenticated version endpoint and reads the bounded deployment record from
// the absolute KAIWU_DATA_DIR. The test binary carries no injected revision, so
// it must report dev rather than a checkout HEAD.
func TestVersionEndpointAppLifecycle(t *testing.T) {
	dir := t.TempDir()
	addr := freeAddr(t)
	token := strings.Repeat("k", 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, config.Config{ListenAddr: addr, Workbench: config.Workbench{Token: token, DataDir: dir}}, discardLogger())
	}()
	client := localClient(t)
	waitForHealth(t, client, addr)
	code, _, cookies := postJSON(t, client, "http://"+addr+"/api/login", "", `{"token":"`+token+`"}`)
	if code != 200 || len(cookies) == 0 {
		t.Fatalf("login failed: %d", code)
	}
	cookie := cookies[0].Name + "=" + cookies[0].Value
	get := func() (int, http.Header, map[string]any) {
		t.Helper()
		req, err := http.NewRequest("GET", "http://"+addr+"/api/version", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", cookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("version body: %q", body)
		}
		return resp.StatusCode, resp.Header, parsed
	}
	if code, header, body := get(); code != 200 || header.Get("Cache-Control") != "no-store" {
		t.Fatalf("version endpoint: %d %v", code, header)
	} else if body["revision"] != "dev" {
		t.Fatalf("development build claimed a revision: %v", body)
	} else if deployment, _ := body["deployment"].(map[string]any); deployment["status"] != "unknown" || body["deployedAt"] != "" {
		t.Fatalf("missing record not reported unknown: %v", body)
	}

	other := strings.Repeat("a", 40)
	record := `{"revision":"` + other + `","deployedAt":"2026-10-19T02:00:00Z","deployment":{"revision":"` + other + `","status":"failed","at":"2026-10-19T02:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "deployment-status.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, body := get()
	deployment, _ := body["deployment"].(map[string]any)
	if deployment["status"] != "failed" || deployment["revision"] != other {
		t.Fatalf("deployment record not read: %v", body)
	}
	if body["deployedAt"] != "" {
		t.Fatalf("foreign revision claimed as deployed: %v", body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}
