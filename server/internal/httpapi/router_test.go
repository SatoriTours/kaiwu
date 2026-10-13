package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func assertHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if h.Get("Content-Type") != "application/json" || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected headers: %v", h)
	}
	if h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS permission: %v", h)
	}
}

func TestHealthGet(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	assertHeaders(t, w.Header())
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]string{"status": "ok", "service": "kaiwu-server"}) {
		t.Fatalf("body=%v", body)
	}
}

func TestHealthHead(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("HEAD", "/healthz", nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	assertHeaders(t, w.Header())
}

func TestHealthHeadContentLength(t *testing.T) {
	h := NewHandler(quietLogger())
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest("GET", "/healthz", nil))
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest("HEAD", "/healthz", nil))
	if get.Body.Len() != 41 || get.Header().Get("Content-Length") != "41" ||
		head.Header().Get("Content-Length") != "41" || head.Body.Len() != 0 {
		t.Fatalf("GET length=%q body=%d; HEAD length=%q body=%d",
			get.Header().Get("Content-Length"), get.Body.Len(), head.Header().Get("Content-Length"), head.Body.Len())
	}
}

func TestJSONEncodingFailureBeforeHeaders(t *testing.T) {
	h := observeAndRecover(quietLogger(), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, http.StatusOK, make(chan int))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/encoding-error", nil))
	assertError(t, w, 500, "internal_error", "internal server error")
}

func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d", w.Code, status)
	}
	assertHeaders(t, w.Header())
	var body map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"error": {"code": code, "message": message}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body=%v want=%v", body, want)
	}
}

func TestNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	assertError(t, w, 404, "not_found", "route not found")
}

func TestMethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("POST", "/healthz", nil))
	assertError(t, w, 405, "method_not_allowed", "method not allowed")
	if w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("Allow=%q", w.Header().Get("Allow"))
	}
}

func TestUnknownMethodRouting(t *testing.T) {
	for _, tc := range []struct {
		path    string
		status  int
		code    string
		message string
		allow   string
	}{
		{"/missing", 404, "not_found", "route not found", ""},
		{"/healthz/", 404, "not_found", "route not found", ""},
		{"/%68ealthz", 404, "not_found", "route not found", ""},
		{"/healthz", 405, "method_not_allowed", "method not allowed", "GET, HEAD"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("CUSTOM", tc.path, nil))
			assertError(t, w, tc.status, tc.code, tc.message)
			if got := w.Header().Get("Allow"); got != tc.allow {
				t.Fatalf("Allow=%q want=%q", got, tc.allow)
			}
		})
	}
}

func TestNotFoundHead(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandler(quietLogger()).ServeHTTP(w, httptest.NewRequest("HEAD", "/missing", nil))
	if w.Code != 404 || w.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	assertHeaders(t, w.Header())
}
