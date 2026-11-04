package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func managedAPI(t *testing.T, enabled bool) (*workbench.Store, string, func(map[string]any) *httptest.ResponseRecorder) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "managed")
	store, err := workbench.Open(filepath.Join(t.TempDir(), "state.db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	h := New(Config{Token: strings.Repeat("x", 32), Host: "127.0.0.1:7331", ProjectsRoot: root, ManagedProjects: enabled}, store, nil)
	t.Cleanup(func() { h.(*handler).Close() })
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
	login := request("/api/login", map[string]any{"token": strings.Repeat("x", 32)})
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie = login.Result().Cookies()[0]
	return store, root, func(body map[string]any) *httptest.ResponseRecorder { return request("/api/commands", body) }
}

func TestManagedProjectAPIIsClosedByDefaultAndNeverWritesFiles(t *testing.T) {
	store, root, call := managedAPI(t, false)
	w := call(map[string]any{"operationId": "create-managed", "type": "create_project", "name": "项目", "revision": store.Snapshot().Revision})
	if w.Code != 503 {
		t.Fatalf("gate: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) || len(store.Snapshot().Projects) != 0 {
		t.Fatal("closed gate has side effects")
	}
}

func TestManagedProjectAPINameOnlyCreatesRealGitAndReplays(t *testing.T) {
	store, root, call := managedAPI(t, true)
	body := map[string]any{"operationId": "create-managed", "type": "create_project", "name": "名字相同", "revision": store.Snapshot().Revision}
	w := call(body)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var r workbench.Receipt
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	p := store.Snapshot().Projects[r.CreatedID]
	if p.StorageMode != "managed" || p.StorageRoot != filepath.Join(root, p.DirectoryName) || gitOutput(t, p.Path, "symbolic-ref", "--short", "HEAD") != "main" {
		t.Fatalf("bad project: %+v", p)
	}
	w = call(body)
	if w.Code != 200 || len(store.Snapshot().Projects) != 1 {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
	// The API never accepts arbitrary client-owned locations for managed creation.
	body["operationId"] = "client-path"
	body["revision"] = store.Snapshot().Revision
	body["path"] = t.TempDir()
	if w = call(body); w.Code != 400 {
		t.Fatalf("path injection: %d %s", w.Code, w.Body.String())
	}
}

func TestRenameProjectAPIPreservesLegacyPaths(t *testing.T) {
	store, _, call := managedAPI(t, false)
	dir := t.TempDir()
	w := call(map[string]any{"operationId": "old-project", "type": "add_project", "name": "原名", "path": dir, "revision": store.Snapshot().Revision})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var r workbench.Receipt
	json.Unmarshal(w.Body.Bytes(), &r)
	p := store.Snapshot().Projects[r.CreatedID]
	w = call(map[string]any{"operationId": "rename-project", "type": "rename_project", "projectId": p.ID, "name": "新名", "revision": store.Snapshot().Revision})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := store.Snapshot().Projects[p.ID]
	p.Name = "新名"
	if got != p || got.StorageMode != "legacy" {
		t.Fatal("rename moved legacy storage")
	}
}
