package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// A reviewed push is requested and observed over the real HTTP routes: the
// browser never names a URL, SHA or credential, and the response only carries the
// persisted operation facts.
// pushRoute is a real local fixture: real repo, real bare remote, the real agent
// manager, and the real HTTP/socket handler. No network and no real credential.
type pushRoute struct {
	root, source, bare string
	store              *workbench.Store
	manager            *agent.Manager
	handler            http.Handler
	project            string
	task               workbench.Task
	call               func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder
}

func pushRouteFixture(t *testing.T) *pushRoute {

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
	git(source, "commit", "-q", "-m", "base")
	git(source, "remote", "add", "origin", "https://github.com/owner/repo.git")
	base := git(source, "rev-parse", "HEAD")
	bare := filepath.Join(root, "remote.git")
	if out, e := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	if out, e := exec.Command("git", "-C", source, "push", "-q", bare, "HEAD:refs/heads/main").CombinedOutput(); e != nil {
		t.Fatal(string(out), e)
	}
	s, e := workbench.Open(filepath.Join(root, "data", "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	r, e := s.Command(workbench.Command{OperationID: "add-project", Revision: s.Snapshot().Revision, Type: "add_project", Path: source, Name: "p"})
	if e != nil {
		t.Fatal(e)
	}
	project := r.CreatedID
	r, e = s.Command(workbench.Command{OperationID: "task", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: project, Request: "feature"})
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
	git(tree, "commit", "-q", "--allow-empty", "-m", "feature")
	_ = s.Update(func(st *workbench.State) error {
		task.Status, task.ExecutionID = "needs_confirmation", "x"
		st.Tasks[task.ID] = task
		c := st.Conversations[task.ConversationID]
		c.RunState = "idle"
		c.Rounds = []workbench.Round{{ID: "round-1", Phase: "finished", StartedAt: "2026-10-24T09:00:00Z", FinishedAt: "2026-10-24T10:00:00Z"}}
		st.Conversations[c.ID] = c
		st.Executions["x"] = workbench.Execution{ID: "x", TaskID: task.ID, ConversationID: c.ID, Workspace: tree, Branch: branch, BaseSHA: base, Phase: "awaiting_review", FinishedAt: "2026-10-24T10:00:00Z"}
		return nil
	})
	resolver, e := githubauth.New(githubauth.Config{Lookup: func(string) (string, bool) { return "token", true }})
	if e != nil {
		t.Fatal(e)
	}
	// Only the frozen legal GitHub URL is redirected at the transport layer.
	transport := gitops.RemoteTransportFunc(func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
		track := gitops.LeaseTrack(lease)
		if !strings.HasPrefix(target.URL, "https://github.com/") {
			return "", errors.New("unexpected url")
		}
		real := make([]string, 0, len(args))
		for _, a := range args {
			if a == target.URL {
				a = bare
			}
			real = append(real, a)
		}
		out, e := gitops.Run(ctx, dir, track, real...)
		return out, e
	})
	m := agent.New(context.Background(), s, agent.Config{DataDir: filepath.Join(root, "data"), Remote: gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver, Transport: transport})})
	t.Cleanup(func() { _ = m.Close() })
	h := New(Config{Host: "127.0.0.1:7331", Token: strings.Repeat("x", 32), Git: m}, s, m).(*handler)
	call := func(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:7331"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kaiwu-Client", "web")
		req.Header.Set("Origin", "http://127.0.0.1:7331")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	return &pushRoute{root: root, source: source, bare: bare, store: s, manager: m, handler: h,
		project: project, task: task, call: call}
}

func TestPushMergeRoutesReturnPersistedFacts(t *testing.T) {
	f := pushRouteFixture(t)
	s, project, task, bare, call := f.store, f.project, f.task, f.bare, f.call
	login := f.call("/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil)
	if login.Code != 200 || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	reviewBody := `{"projectId":"` + project + `","taskId":"` + task.ID + `","mode":"push","roundId":"round-1"}`
	w := call("/api/git/preview", reviewBody, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var review workbench.MergeReview
	if e := json.Unmarshal(w.Body.Bytes(), &review); e != nil {
		t.Fatal(e)
	}
	if !review.CanPush || review.AlreadyMerged || review.RemoteURL != "https://github.com/owner/repo.git" || len(review.Uploads) != 1 {
		t.Fatalf("review: %#v", review)
	}
	mergeBody := `{"operationId":"http-push","projectId":"` + project + `","taskId":"` + task.ID + `","previewId":"` + review.ID + `","commitMessage":"feature","mode":"push","roundId":"round-1"}`
	w = call("/api/git/merge", mergeBody, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	var op workbench.MergeOperation
	if e := json.Unmarshal(w.Body.Bytes(), &op); e != nil {
		t.Fatal(e)
	}
	if op.Phase != "queued" || op.Mode != workbench.MergeModePush || op.Push == nil || op.IntendedSHA != "" {
		t.Fatalf("admission: %#v", op)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got := s.Snapshot().Merges["http-push"]
		if !workbench.MergeActive(got.Phase) {
			if got.Phase != "succeeded" || got.RemoteAfterSHA == "" || got.CIStatus != "not_checked" {
				t.Fatalf("result: %#v", got)
			}
			if out, _ := exec.Command("git", "-C", bare, "rev-parse", "refs/heads/main").Output(); strings.TrimSpace(string(out)) != got.IntendedSHA {
				t.Fatalf("remote %q != intended", out)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("push did not finish")
}

// A socket-issued push review/merge must expose the same bounded facts as HTTP:
// the queued state first, then the terminal phase, with no URL/SHA/credential in
// the request or the reply, and CI never claimed as checked.
func TestPushMergeSocketFactsAreBounded(t *testing.T) {
	f := pushRouteFixture(t)
	login := f.call("/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil)
	cookie := login.Result().Cookies()[0]
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	h := New(Config{Host: srv.Listener.Addr().String(), Token: strings.Repeat("x", 32), Git: f.manager}, f.store, f.manager).(*handler)
	srv.Config.Handler = h
	srv.Start()
	defer srv.Close()
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, strings.Replace(srv.URL, "http:", "ws:", 1)+"/api/socket",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}, "Cookie": {cookie.String()}}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	send := func(id, method, body string) map[string]any {
		t.Helper()
		if body == "" {
			body = "{}"
		}
		b, e := json.Marshal(map[string]any{"id": id, "method": method, "body": json.RawMessage(body)})
		if e != nil {
			t.Fatal(e)
		}
		if e := c.Write(ctx, websocket.MessageText, b); e != nil {
			t.Fatal(e)
		}
		var data []byte
		for {
			// Progress/snapshot frames may interleave; only the reply is mine.
			_, data, e = c.Read(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var probe struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			if json.Unmarshal(data, &probe) == nil && probe.Type == "reply" && probe.ID == id {
				break
			}
			t.Logf("interleaved frame for %q: %.120s", id, data)
		}
		var reply struct {
			ID     string          `json:"id"`
			Status int             `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if e := json.Unmarshal(data, &reply); e != nil {
			t.Fatalf("%s: %s", data, e)
		}
		if reply.ID != id || (reply.Status != 200 && reply.Status != http.StatusAccepted) {
			t.Fatalf("%s: %d %s", id, reply.Status, reply.Data)
		}
		var out map[string]any
		if e := json.Unmarshal(reply.Data, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	review := send("r1", "git/preview", `{"projectId":"`+f.project+`","taskId":"`+f.task.ID+`","mode":"push","roundId":"round-1"}`)
	if review["canPush"] != true {
		t.Fatalf("review: %#v", review)
	}
	id, _ := review["id"].(string)
	if id == "" {
		t.Fatalf("review id: %#v", review)
	}
	queued := send("m1", "git/merge", `{"operationId":"socket-push","projectId":"`+f.project+`","taskId":"`+f.task.ID+`","previewId":"`+id+`","commitMessage":"feature","mode":"push","roundId":"round-1"}`)
	if queued["phase"] != "queued" || queued["intendedSHA"] != "" && queued["intendedSHA"] != nil {
		t.Fatalf("queued facts: %#v", queued)
	}
	if queued["remoteAfterSHA"] != nil && queued["remoteAfterSHA"] != "" {
		t.Fatalf("queued invented remote success: %#v", queued)
	}
	if queued["push"].(map[string]any)["mainRef"] == "" {
		t.Fatalf("queued freeze missing: %#v", queued)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		got := send("s1", "git/operations/socket-push", "")
		if phase, _ := got["phase"].(string); !workbench.MergeActive(phase) {
			if phase != "succeeded" {
				t.Fatalf("terminal: %#v", got)
			}
			if got["ciStatus"] != "not_checked" {
				t.Fatalf("CI claimed checked: %#v", got)
			}
			intended, _ := got["intendedSHA"].(string)
			mainAfter, _ := got["mainAfterSHA"].(string)
			remoteAfter, _ := got["remoteAfterSHA"].(string)
			if len(intended) != 40 || intended != mainAfter || intended != remoteAfter {
				t.Fatalf("fact SHAs %q %q %q", intended, mainAfter, remoteAfter)
			}
			if out, _ := exec.Command("git", "-C", f.bare, "rev-parse", "refs/heads/main").Output(); strings.TrimSpace(string(out)) != intended {
				t.Fatalf("remote %q != %q", out, intended)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("push did not finish: %#v", got)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Local merge mode must never start a remote write and must not claim any
// remote fact, while the legacy (non-push) request shape still works.
func TestLocalMergeNeverWritesRemote(t *testing.T) {
	f := pushRouteFixture(t)
	login := f.call("/api/login", `{"token":"`+strings.Repeat("x", 32)+`"}`, nil)
	cookie := login.Result().Cookies()[0]
	// Legacy local flow: PreviewMerge then a merge body without any of the new
	// push fields, which must keep working unchanged.
	rv, e := f.manager.PreviewMerge(context.Background(), f.project, f.task.ID)
	if e != nil || !rv.CanMerge {
		t.Fatal(rv, e)
	}
	before, _ := exec.Command("git", "-C", f.bare, "rev-parse", "refs/heads/main").Output()
	merge := f.call("/api/git/merge", `{"operationId":"local-legacy","projectId":"`+f.project+`","taskId":"`+f.task.ID+`","previewId":"`+rv.ID+`","commitMessage":"feature"}`, cookie)
	if merge.Code != http.StatusAccepted {
		t.Fatal(merge.Code, merge.Body.String())
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		op := f.store.Snapshot().Merges["local-legacy"]
		if !workbench.MergeActive(op.Phase) {
			// A legacy body carries no mode, so the honest record is a local
			// merge either way; what matters is that no remote work happened.
			if op.Phase != "succeeded" || (op.Mode != "" && op.Mode != workbench.MergeModeLocal) {
				t.Fatalf("local merge: %#v", op)
			}
			if op.RemoteAfterSHA != "" || op.IntendedSHA != "" || op.PushAttemptedAt != "" || op.Push != nil || op.CIStatus == "success" {
				t.Fatalf("local merge claimed remote facts: %#v", op)
			}
			after, _ := exec.Command("git", "-C", f.bare, "rev-parse", "refs/heads/main").Output()
			if strings.TrimSpace(string(before)) != strings.TrimSpace(string(after)) {
				t.Fatal("local merge wrote the remote")
			}
			_ = rv
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("local merge did not finish: %#v", op)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
