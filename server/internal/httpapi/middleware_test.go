package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// lockedLog keeps concurrent request logging separate from test data races.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func capturedLogger() (*slog.Logger, *lockedLog) {
	logs := &lockedLog{}
	return slog.New(slog.NewJSONHandler(logs, nil)), logs
}

func requestEntry(t *testing.T, logs *lockedLog) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON log: %v", err)
		}
		if entry["msg"] == "http request" {
			return entry
		}
	}
	t.Fatalf("missing request log: %s", logs.String())
	return nil
}

func TestPanicBeforeWrite(t *testing.T) {
	logger, logs := capturedLogger()
	h := observeAndRecover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("panic-private-secret")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	assertError(t, w, 500, "internal_error", "internal server error")
	if strings.Contains(logs.String(), "panic-private-secret") || !strings.Contains(logs.String(), "handler panic") {
		t.Fatalf("unsafe or missing panic log: %s", logs.String())
	}
	if requestEntry(t, logs)["status"] != float64(500) {
		t.Fatal("panic request did not log status 500")
	}
}

func TestPanicAfterWrite(t *testing.T) {
	logger, logs := capturedLogger()
	h := observeAndRecover(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("panic-private-secret")
	}))
	w := httptest.NewRecorder()
	var caught any
	func() {
		defer func() { caught = recover() }()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	}()
	if caught != http.ErrAbortHandler {
		t.Fatalf("panic=%v want ErrAbortHandler", caught)
	}
	if w.Code != 200 || w.Body.String() != "partial" {
		t.Fatalf("corrupted partial response: %d %q", w.Code, w.Body.String())
	}
	if strings.Contains(logs.String(), "panic-private-secret") {
		t.Fatal("panic value leaked")
	}
	if requestEntry(t, logs)["status"] != float64(200) {
		t.Fatal("partial response log lost committed status")
	}
}

func TestRequestLogPrivacy(t *testing.T) {
	logger, logs := capturedLogger()
	r := httptest.NewRequest("POST", "/healthz?secret=query-secret", strings.NewReader("body-secret"))
	r.Header.Set("Authorization", "Bearer header-secret")
	w := httptest.NewRecorder()
	NewHandler(logger).ServeHTTP(w, r)
	for _, secret := range []string{"query-secret", "body-secret", "header-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log leaked %s", secret)
		}
	}
	entry := requestEntry(t, logs)
	if entry["method"] != "POST" || entry["path"] != "/healthz" || entry["status"] != float64(405) || entry["duration"] == nil {
		t.Fatalf("incomplete request log: %v", entry)
	}
}

func TestRequestLogStatus(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/healthz", 200}, {"GET", "/missing", 404}, {"POST", "/healthz", 405}, {"GET", "/panic", 500},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			logger, logs := capturedLogger()
			var h http.Handler = NewHandler(logger)
			if tc.status == 500 {
				h = observeAndRecover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("test") }))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || requestEntry(t, logs)["status"] != float64(tc.status) {
				t.Fatalf("response/log status mismatch: %d %s", w.Code, logs.String())
			}
		})
	}
}

func TestAbortHandlerIsPreserved(t *testing.T) {
	logger, logs := capturedLogger()
	h := observeAndRecover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	w := httptest.NewRecorder()
	var caught any
	func() {
		defer func() { caught = recover() }()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/abort", nil))
	}()
	if caught != http.ErrAbortHandler || w.Body.Len() != 0 {
		t.Fatalf("abort corrupted: panic=%v body=%q", caught, w.Body.String())
	}
	if strings.Contains(logs.String(), "handler panic") {
		t.Fatal("intentional abort reported as panic")
	}
}

func TestResponseControllerUnwrap(t *testing.T) {
	h := observeAndRecover(quietLogger(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush() = %v", err)
		}
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/flush", nil))
	if !w.Flushed {
		t.Fatal("wrapper did not forward response controller")
	}
}

func TestPanicAfterFlush(t *testing.T) {
	logger, logs := capturedLogger()
	h := observeAndRecover(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		panic("test")
	}))
	w := httptest.NewRecorder()
	var caught any
	func() {
		defer func() { caught = recover() }()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/flush", nil))
	}()
	if caught != http.ErrAbortHandler || w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("flushed response corrupted: panic=%v status=%d body=%q", caught, w.Code, w.Body.String())
	}
	if requestEntry(t, logs)["status"] != float64(200) {
		t.Fatal("flush did not track committed response")
	}
}

func TestPanicAbortsRealConnection(t *testing.T) {
	h := observeAndRecover(quietLogger(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("test")
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := srv.Client()
	resp, err := client.Get(srv.URL)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("connection succeeded after aborted unflushed response")
	}
	if errors.Is(err, http.ErrHandlerTimeout) {
		t.Fatalf("unexpected timeout: %v", err)
	}
}
