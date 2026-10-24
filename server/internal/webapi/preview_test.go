package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestPreviewHTTPAndSocketAdmission(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p"}
		st.Tasks["t"] = workbench.Task{ID: "t", ProjectID: "p", ConversationID: "c"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle"}
		return nil
	})
	pm := preview.New(context.Background(), s, preview.Config{DataDir: t.TempDir()})
	defer pm.Close()
	token := strings.Repeat("x", 32)
	h := New(Config{Token: token, Host: "127.0.0.1:7331", Previews: pm}, s, nil).(*handler)
	defer h.Close()
	call := func(path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:7331"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Kaiwu-Client", "web")
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"projectId":"p","taskId":"t","roundId":"r"}`
	if w := call("/api/previews/status", body, "http://127.0.0.1:7331", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	login := call("/api/login", `{"token":"`+token+`"}`, "http://127.0.0.1:7331", nil)
	cookie := login.Result().Cookies()[0]
	if w := call("/api/previews/status", body, "null", cookie); w.Code != 403 {
		t.Fatal("opaque origin reached API", w.Code)
	}
	w := call("/api/previews/status", body, "http://127.0.0.1:7331", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unavailable"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7331/api/socket", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "http://127.0.0.1:7331")
	for _, method := range []string{"previews/status", "previews/start", "previews/stop", "previews/open"} {
		reply := h.socketCall(r, socketRequest{ID: method, Method: method, Body: json.RawMessage(body)})
		if strings.Contains(string(reply.Data), "不支持的实时请求") || reply.Status == 404 {
			t.Fatal(method, reply)
		}
	}
	gateway := httptest.NewRequest("GET", "http://127.0.0.1:7331/preview/"+strings.Repeat("a", 64)+"/", nil)
	gateway.Header.Set("Origin", "null")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, gateway)
	if out.Code != 401 {
		t.Fatal("preview token not checked", out.Code)
	}
}

func TestPreviewGatewayHTTPHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "webapi-preview-helper" {
		return
	}
	ln, e := net.Listen("tcp", "127.0.0.1:"+os.Args[len(os.Args)-1])
	if e != nil {
		os.Exit(2)
	}
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "task-cookie=not-workbench")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "cookie=%s;authorization=%s", r.Header.Get("Cookie"), r.Header.Get("Authorization"))
	}))
	os.Exit(0)
}

func TestPreviewGatewayCapabilityWorksWithoutCookieAndRevokesWithLogin(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	_ = os.Mkdir(source, 0700)
	git := func(args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "--template=", "-b", "main")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	base := git("rev-parse", "HEAD")
	data := filepath.Join(root, "data")
	id := strings.Repeat("f", 32)
	parent := filepath.Join(data, "worktrees", id)
	_ = os.MkdirAll(parent, 0700)
	tree := filepath.Join(parent, "tree")
	branch := "kaiwu/task-" + id
	git("worktree", "add", "-b", branch, tree)
	owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	_ = os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
	exe, _ := os.Executable()
	manifest, _ := json.Marshal(map[string]any{"version": 1, "command": []string{exe, "-test.run=TestPreviewGatewayHTTPHelper", "--", "webapi-preview-helper", "{port}"}, "path": "/"})
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), manifest, 0600)
	s, e := workbench.Open(filepath.Join(data, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: source}
		st.Tasks[id] = workbench.Task{ID: id, ProjectID: "p", ConversationID: "c", ExecutionID: "x", Status: "completed"}
		st.Executions["x"] = workbench.Execution{ID: "x", TaskID: id, ConversationID: "c", Workspace: tree, Branch: branch, BaseSHA: base, Phase: "confirmed", FinishedAt: "done"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", ProjectID: "p", TaskID: id, RunState: "idle", Rounds: []workbench.Round{{ID: "r", Phase: "confirmed", FinishedAt: "done"}}}
		return nil
	})
	pm := preview.New(context.Background(), s, preview.Config{DataDir: data})
	defer pm.Close()
	accessToken := strings.Repeat("x", 32)
	credential := strings.Repeat("a", 64)
	loginHash := credentialHash(credential)
	if e = s.SaveLogin(loginHash, credentialHash(accessToken), time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	h := New(Config{Token: accessToken, Host: "127.0.0.1:7331", Previews: pm}, s, nil).(*handler)
	defer h.Close()
	req := preview.Request{ProjectID: "p", TaskID: id, RoundID: "r", OperationID: "explicit-preview"}
	shown, err := pm.Status(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ManifestID = shown.ManifestID
	if v, e := pm.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	v, e := pm.Open(req, loginHash)
	if e != nil {
		t.Fatal(e)
	}
	call := func(origin, host, method string, withCookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+v.URL, nil)
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Authorization", "Bearer private")
		if withCookie {
			r.AddCookie(&http.Cookie{Name: "kaiwu_session", Value: credential})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, withCookie := range []bool{false, true} {
		w := call("null", "127.0.0.1:7331", "GET", withCookie)
		if w.Code != 200 || w.Body.String() != "cookie=;authorization=" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("X-Frame-Options") != "" || w.Header().Get("Access-Control-Allow-Origin") != "null" {
			t.Fatal("capability isolation failed", w.Code, w.Body.String(), w.Header())
		}
	}
	if w := call("http://evil.invalid", "127.0.0.1:7331", "GET", false); w.Code != 403 {
		t.Fatal("external origin", w.Code)
	}
	if w := call("null", "evil.invalid:7331", "GET", false); w.Code != 403 {
		t.Fatal("external host", w.Code)
	}
	if w := call("null", "127.0.0.1:7331", "POST", true); w.Code != 405 {
		t.Fatal("preview wrote", w.Code)
	}
	if e = s.RevokeLogin(loginHash); e != nil {
		t.Fatal(e)
	}
	if w := call("null", "127.0.0.1:7331", "GET", false); w.Code != 401 {
		t.Fatal("revoked login kept preview access", w.Code)
	}
	if s.Snapshot().Tasks[id].Status != "completed" {
		t.Fatal("preview changed confirmation")
	}
}

// TestPreviewArtifactDownloadOverHTTP proves the declared-file download works
// through the real HTTP admission layer: capability-only access, forced
// attachment, and login revocation. Nothing starts and no task state changes.
func TestPreviewArtifactDownloadOverHTTP(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	_ = os.Mkdir(source, 0700)
	git := func(args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "--template=", "-b", "main")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	base := git("rev-parse", "HEAD")
	data := filepath.Join(root, "data")
	id := strings.Repeat("e", 32)
	parent := filepath.Join(data, "worktrees", id)
	_ = os.MkdirAll(parent, 0700)
	tree := filepath.Join(parent, "tree")
	branch := "kaiwu/task-" + id
	git("worktree", "add", "-b", branch, tree)
	owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	_ = os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
	_ = os.MkdirAll(filepath.Join(tree, "out"), 0o755)
	_ = os.WriteFile(filepath.Join(tree, "out", "报告.csv"), []byte("a,b\n1,2\n"), 0600)
	manifest, _ := json.Marshal(map[string]any{"version": 2, "kind": "files", "artifacts": []map[string]any{{"path": "out/报告.csv", "name": "报告 数据.csv"}}})
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), manifest, 0600)
	s, e := workbench.Open(filepath.Join(data, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_ = s.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: source}
		st.Tasks[id] = workbench.Task{ID: id, ProjectID: "p", ConversationID: "c", ExecutionID: "x", Status: "completed"}
		st.Executions["x"] = workbench.Execution{ID: "x", TaskID: id, ConversationID: "c", Workspace: tree, Branch: branch, BaseSHA: base, Phase: "confirmed", FinishedAt: "done"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", ProjectID: "p", TaskID: id, RunState: "idle", Rounds: []workbench.Round{{ID: "r", Phase: "confirmed", FinishedAt: "done"}}}
		return nil
	})
	pm := preview.New(context.Background(), s, preview.Config{DataDir: data})
	defer pm.Close()
	accessToken := strings.Repeat("x", 32)
	credential := strings.Repeat("a", 64)
	loginHash := credentialHash(credential)
	if e = s.SaveLogin(loginHash, credentialHash(accessToken), time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	h := New(Config{Token: accessToken, Host: "127.0.0.1:7331", Previews: pm}, s, nil).(*handler)
	defer h.Close()
	req := preview.Request{ProjectID: "p", TaskID: id, RoundID: "r", OperationID: "artifact-preview"}
	shown, err := pm.Status(req)
	if err != nil || shown.Kind != "files" || len(shown.Artifacts) != 1 || shown.URL != "" || shown.Artifacts[0].URL != "" {
		t.Fatal(shown, err)
	}
	req.ManifestID = shown.ManifestID
	v, e := pm.Open(req, loginHash)
	if e != nil || len(v.Artifacts) != 1 || !strings.Contains(v.Artifacts[0].URL, "/~download/") {
		t.Fatal(v, e)
	}
	fetch := func(method, target string, withCookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7331"+target, nil)
		r.Host = "127.0.0.1:7331"
		r.Header.Set("Origin", "null")
		if withCookie {
			r.AddCookie(&http.Cookie{Name: "kaiwu_session", Value: credential})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, withCookie := range []bool{false, true} {
		w := fetch("GET", v.Artifacts[0].URL, withCookie)
		if w.Code != 200 || w.Body.String() != "a,b\n1,2\n" || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" ||
			!strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") ||
			!strings.Contains(w.Header().Get("Content-Disposition"), "filename*=UTF-8''%E6%8A%A5%E5%91%8A%20%E6%95%B0%E6%8D%AE.csv") ||
			w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" ||
			w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("download failed", w.Code, w.Body.String(), w.Header())
		}
	}
	if w := fetch("HEAD", v.Artifacts[0].URL, false); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "8" {
		t.Fatal("head", w.Code, w.Body.Len(), w.Header())
	}
	if w := fetch("GET", "/preview/"+strings.Repeat("b", 64)+"/~download/"+v.Artifacts[0].ID, false); w.Code == 200 {
		t.Fatal("unknown capability served", w.Code)
	}
	if w := fetch("GET", "/api/previews/status", false); w.Code == 405 && false {
		t.Fatal("unreachable")
	}
	if e = s.RevokeLogin(loginHash); e != nil {
		t.Fatal(e)
	}
	if w := fetch("GET", v.Artifacts[0].URL, false); w.Code != 401 {
		t.Fatal("revoked login downloaded", w.Code)
	}
	if st := s.Snapshot(); st.Tasks[id].Status != "completed" || len(st.Executions) != 1 {
		t.Fatal("download changed task state")
	}
}
