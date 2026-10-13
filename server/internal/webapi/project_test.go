package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func projectAPI(t *testing.T) (*workbench.Store, func(map[string]any) *httptest.ResponseRecorder) {
	t.Helper()
	s, err := workbench.Open(filepath.Join(t.TempDir(), "state.db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := New(Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331"}, s, nil)
	var cookie *http.Cookie
	request := func(path string, body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "http://127.0.0.1:7331"+path, strings.NewReader(string(b)))
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
	w := request("/api/login", map[string]any{"token": strings.Repeat("x", 32)})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie = w.Result().Cookies()[0]
	return s, func(c map[string]any) *httptest.ResponseRecorder { return request("/api/commands", c) }
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	b, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, b, err)
	}
	return strings.TrimSpace(string(b))
}

func TestAddProjectInitializesWithoutRemoteAndPreservesFiles(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing directory", true: "new directory"}[missing], func(t *testing.T) {
			s, call := projectAPI(t)
			dir := filepath.Join(t.TempDir(), "new project")
			if !missing {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Both old and new forms can send only a name and path.
			c := map[string]any{"operationId": "init-project", "revision": s.Snapshot().Revision, "type": "add_project", "name": "新项目", "path": dir}
			w := call(c)
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if gitOutput(t, dir, "rev-parse", "--show-toplevel") != dir {
				t.Fatal("wrong repository root")
			}
			if gitOutput(t, dir, "remote") != "" {
				t.Fatal("remote is required or invented")
			}
			if err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Run(); err == nil {
				t.Fatal("unexpected commit")
			}
			if !missing {
				b, _ := os.ReadFile(filepath.Join(dir, "note.txt"))
				if string(b) != "keep me" {
					t.Fatal("existing files changed")
				}
			}
			if call(c).Code != 200 || len(s.Snapshot().Projects) != 1 {
				t.Fatal("retry must replay, not duplicate")
			}
		})
	}
}

func TestAddProjectOptionalRemoteAndExistingRepository(t *testing.T) {
	s, call := projectAPI(t)
	dir := t.TempDir()
	gitOutput(t, dir, "init", "--template=")
	gitOutput(t, dir, "remote", "add", "origin", "https://example.com/original.git")
	c := map[string]any{"operationId": "existing-project", "revision": s.Snapshot().Revision, "type": "add_project", "name": "已有项目", "path": dir, "remoteURL": "https://example.com/different.git"}
	if w := call(c); w.Code != 409 {
		t.Fatalf("must preserve existing origin: %d %s", w.Code, w.Body.String())
	}
	if gitOutput(t, dir, "remote", "get-url", "origin") != "https://example.com/original.git" {
		t.Fatal("origin overwritten")
	}
	c["remoteURL"] = ""
	if w := call(c); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	dir2 := filepath.Join(t.TempDir(), "new")
	c = map[string]any{"operationId": "remote-project", "revision": s.Snapshot().Revision, "type": "add_project", "name": "远程可选", "path": dir2, "initializeGit": "on", "remoteURL": "git@example.com:team/repo.git"}
	if w := call(c); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if gitOutput(t, dir2, "remote", "get-url", "origin") != "git@example.com:team/repo.git" {
		t.Fatal("remote not configured")
	}
	// Replaying a receipt must not repeat filesystem preparation.
	gitOutput(t, dir2, "remote", "set-url", "origin", "https://example.com/changed.git")
	if w := call(c); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if gitOutput(t, dir2, "remote", "get-url", "origin") != "https://example.com/changed.git" {
		t.Fatal("replay modified origin")
	}
}

func TestInvalidProjectDoesNotInitialize(t *testing.T) {
	for _, reason := range []string{"empty name", "stale revision", "invalid remote", "relative path"} {
		t.Run(reason, func(t *testing.T) {
			s, call := projectAPI(t)
			dir := filepath.Join(t.TempDir(), "untouched")
			c := map[string]any{"operationId": "invalid-project", "revision": s.Snapshot().Revision, "type": "add_project", "name": "test", "path": dir, "initializeGit": "on"}
			switch reason {
			case "empty name":
				c["name"] = ""
			case "stale revision":
				c["revision"] = 0
			case "invalid remote":
				c["remoteURL"] = "--upload-pack=bad"
			case "relative path":
				c["path"] = "relative"
			}
			if w := call(c); w.Code < 400 {
				t.Fatal("invalid project accepted")
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("invalid request created directory")
			}
			if len(s.Snapshot().Projects) != 0 {
				t.Fatal("invalid request persisted")
			}
		})
	}
}
