package webapi

import (
	"context"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitEndpointsRequireAuthAndOrigin(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	os.Mkdir(source, 0700)
	if b, e := exec.Command("git", "init", "--initial-branch=main", "--template=", source).CombinedOutput(); e != nil {
		t.Fatal(string(b), e)
	}
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Command(workbench.Command{OperationID: "add-project", Revision: s.Snapshot().Revision, Type: "add_project", Path: source, Name: "p"})
	if e != nil {
		t.Fatal(e)
	}
	p := r.CreatedID
	manager := agent.New(context.Background(), s, agent.Config{DataDir: filepath.Join(root, "data")})
	defer manager.Close()
	h := New(Config{Host: "127.0.0.1:7331", Token: strings.Repeat("x", 32), Git: manager}, s, manager).(*handler)
	call := func(path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:7331"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kaiwu-Client", "web")
		req.Header.Set("Origin", origin)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	login := call("/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, "http://127.0.0.1:7331", nil)
	cookie := login.Result().Cookies()[0]
	body := `{"projectId":"` + p + `"}`
	if w := call("/api/git/refresh", body, "http://127.0.0.1:7331", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("/api/git/refresh", body, "http://evil.test", cookie); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("/api/git/refresh", body, "http://127.0.0.1:7331", cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("/api/git/preview", `{"projectId":"`+p+`","taskId":"foreign"}`, "http://127.0.0.1:7331", cookie); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("/api/git/preview", `{"projectId":"`+p+`","taskId":"foreign","path":"/tmp"}`, "http://127.0.0.1:7331", cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
	command := workbench.Command{OperationID: "settings", Type: "update_project_git", Revision: s.Snapshot().Revision, ProjectID: p, CommitName: "Human", CommitEmail: "human@example.invalid"}
	b, _ := json.Marshal(command)
	if w := call("/api/commands", string(b), "http://127.0.0.1:7331", cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.Snapshot().Projects[p].CommitName != "Human" {
		t.Fatal("setting not saved")
	}
}
func TestMergeOutlivesRequestAndStoreClosesLast(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "project")
	os.Mkdir(source, 0700)
	git := func(dir string, args ...string) string {
		t.Helper()
		out, e := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatal(string(out), e)
		}
		return strings.TrimSpace(string(out))
	}
	git(source, "init", "--initial-branch=main", "--template=")
	os.WriteFile(filepath.Join(source, "base"), []byte("base"), 0600)
	git(source, "add", ".")
	git(source, "commit", "-m", "base")
	base := git(source, "rev-parse", "HEAD")
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Command(workbench.Command{OperationID: "project", Revision: s.Snapshot().Revision, Type: "add_project", Name: "p", Path: source})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Command(workbench.Command{OperationID: "task", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: p.CreatedID, Request: "feature"})
	if e != nil {
		t.Fatal(e)
	}
	task := s.Snapshot().Tasks[r.CreatedID]
	branch := "kaiwu/task-" + task.ID
	parent := filepath.Join(root, "data", "worktrees", task.ID)
	os.MkdirAll(parent, 0700)
	tree := filepath.Join(parent, "tree")
	git(source, "worktree", "add", "-b", branch, tree, base)
	owner, _ := json.Marshal(map[string]any{"TaskID": task.ID, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
	os.WriteFile(filepath.Join(tree, "feature"), []byte("feature"), 0600)
	_ = s.Update(func(st *workbench.State) error {
		task.ExecutionID = "x"
		st.Tasks[task.ID] = task
		st.Executions["x"] = workbench.Execution{ID: "x", Workspace: tree, Branch: branch, BaseSHA: base}
		return nil
	})
	m := agent.New(context.Background(), s, agent.Config{DataDir: filepath.Join(root, "data")})
	defer m.Close()
	h := New(Config{Host: "127.0.0.1:7331", Token: strings.Repeat("x", 32), Git: m}, s, m).(*handler)
	loginRequest := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"token":"`+strings.Repeat("x", 32)+`"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	login := httptest.NewRecorder()
	h.login(login, loginRequest)
	cookie := login.Result().Cookies()[0]
	proof, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !proof.CanMerge {
		t.Fatal(proof, e)
	}
	request := workbench.MergeRequest{OperationID: "http-merge", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: proof.ID, CommitMessage: "feature"}
	body, _ := json.Marshal(request)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/git/merge", strings.NewReader(string(body))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:7331")
	req.Header.Set("X-Kaiwu-Client", "web")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	cancel()
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op := s.Snapshot().Merges[request.OperationID]
		if !workbench.MergeActive(op.Phase) {
			if op.Phase != "succeeded" {
				t.Fatal(op)
			}
			if e = m.Close(); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(filepath.Join(source, "feature")); e != nil {
				t.Fatal(e)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("request cancellation stopped merge")
}
