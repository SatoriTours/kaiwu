package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/buildinfo"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const versionRev = "8b54309f03dd75759291be48ff3f3b766ab89b5e"

// versionClient serves one handler with a logged-in cookie for /api/version.
type versionClient struct {
	h      http.Handler
	cookie *http.Cookie
	token  string
}

func newVersionClient(t *testing.T, cfg Config) *versionClient {
	t.Helper()
	s, err := workbench.Open(filepath.Join(t.TempDir(), "s.db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	cfg.Token = strings.Repeat("x", 32)
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1:7331"
	}
	h := New(cfg, s, nil)
	c := &versionClient{h: h, token: cfg.Token}
	w := c.call("POST", "/api/login", `{"token":"`+cfg.Token+`"}`, "http://127.0.0.1:7331")
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies: %v", cookies)
	}
	c.cookie = cookies[0]
	return c
}

func (c *versionClient) call(method, path, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7331"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Kaiwu-Client", "web")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if c.cookie != nil && path != "/api/login" {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func (c *versionClient) version(t *testing.T) (int, http.Header, map[string]any) {
	t.Helper()
	w := c.call("GET", "/api/version", "", "")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode version: %v (%s)", err, w.Body.String())
	}
	return w.Code, w.Header(), body
}

func TestVersionRequiresLoginAndSameOrigin(t *testing.T) {
	c := newVersionClient(t, Config{})
	noCookie := &versionClient{h: c.h}
	if w := noCookie.call("GET", "/api/version", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	if w := c.call("GET", "/api/version", "", "http://evil.test"); w.Code != 403 {
		t.Fatalf("cross-origin: %d", w.Code)
	}
}

func TestVersionIsUncachedAndReportsRunningBinary(t *testing.T) {
	c := newVersionClient(t, Config{
		Version: buildinfo.Info{Revision: versionRev, ShortRevision: versionRev[:7], Summary: "修复部署", BuiltAt: "2026-10-19T02:00:00Z"},
		Status: func() buildinfo.DeploymentStatus {
			return buildinfo.DeploymentStatus{
				Revision:   versionRev,
				DeployedAt: "2026-10-19T02:05:00Z",
				Deployment: buildinfo.Deployment{Revision: versionRev, Status: buildinfo.StatusSucceeded, At: "2026-10-19T02:05:00Z"},
			}
		},
	})
	code, header, body := c.version(t)
	if code != 200 {
		t.Fatalf("status: %d %v", code, body)
	}
	if header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cacheable: %q", header.Get("Cache-Control"))
	}
	if body["revision"] != versionRev || body["shortRevision"] != versionRev[:7] {
		t.Fatalf("revision: %v", body)
	}
	if body["summary"] != "修复部署" || body["builtAt"] != "2026-10-19T02:00:00Z" || body["deployedAt"] != "2026-10-19T02:05:00Z" {
		t.Fatalf("metadata: %v", body)
	}
	deployment, ok := body["deployment"].(map[string]any)
	if !ok {
		t.Fatalf("deployment missing: %v", body)
	}
	if deployment["revision"] != versionRev || deployment["status"] != "succeeded" || deployment["at"] != "2026-10-19T02:05:00Z" {
		t.Fatalf("deployment: %v", deployment)
	}
	if deployment["message"] != "部署成功" {
		t.Fatalf("message: %v", deployment)
	}
}

func TestVersionUnknownWithoutStatus(t *testing.T) {
	c := newVersionClient(t, Config{Version: buildinfo.Info{Revision: versionRev, ShortRevision: versionRev[:7]}})
	_, header, body := c.version(t)
	if header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cacheable: %q", header.Get("Cache-Control"))
	}
	if body["revision"] != versionRev || body["deployedAt"] != "" {
		t.Fatalf("revision: %v", body)
	}
	deployment, _ := body["deployment"].(map[string]any)
	if deployment["status"] != "unknown" || deployment["message"] == "" || deployment["revision"] != "" || deployment["at"] != "" {
		t.Fatalf("deployment: %v", deployment)
	}
}

func TestVersionNeverClaimsSuccessWithoutEvidence(t *testing.T) {
	c := newVersionClient(t, Config{
		Version: buildinfo.Info{Revision: versionRev, ShortRevision: versionRev[:7]},
		Status: func() buildinfo.DeploymentStatus {
			// A hand-edited status file can carry a status but no usable revision.
			return buildinfo.DeploymentStatus{Deployment: buildinfo.Deployment{Status: buildinfo.StatusSucceeded}}
		},
	})
	_, _, body := c.version(t)
	deployment, _ := body["deployment"].(map[string]any)
	if deployment["status"] != "unknown" {
		t.Fatalf("unsupported success claimed: %v", deployment)
	}
}
