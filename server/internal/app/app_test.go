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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/config"
	"github.com/SatoriTours/kaiwu/server/internal/httpapi"
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
