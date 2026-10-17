//go:build linux

package preview

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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// A real isolated HTTP process, never pi or a model.
func TestHTTPHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-3] != "preview-helper" {
		return
	}
	mode, port := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if mode == "changed" {
		_ = os.WriteFile("approved-changed", []byte("explicitly approved"), 0600)
	}
	if strings.HasPrefix(mode, "orphan-watch:") {
		parent := strings.TrimPrefix(mode, "orphan-watch:")
		_ = os.WriteFile("descendant-ready", []byte(strconv.Itoa(os.Getpid())), 0600)
		for {
			if _, e := os.Stat("/proc/" + parent); os.IsNotExist(e) {
				_ = os.WriteFile("leader-reaped-before-descendant", []byte("unsafe"), 0600)
			}
			time.Sleep(time.Millisecond)
		}
	}
	if mode == "leader-exit" {
		cmd := exec.Command(os.Args[0], "-test.run=TestHTTPHelper", "--", "preview-helper", "orphan-watch:"+strconv.Itoa(os.Getpid()), port)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Start() != nil {
			os.Exit(2)
		}
		for {
			if _, e := os.Stat("descendant-ready"); e == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		os.Exit(0)
	}
	if mode == "timeout" {
		fmt.Print(strings.Repeat("x", 128<<10))
		select {}
	}
	host := "127.0.0.1"
	if mode == "wildcard" {
		host = "0.0.0.0"
	}
	ln, e := net.Listen("tcp", net.JoinHostPort(host, port))
	if e != nil {
		os.Exit(2)
	}
	fmt.Println("listening")
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://example.invalid/", 302)
			return
		}
		if r.URL.Path == "/local-redirect" {
			http.Redirect(w, r, "/hello", 302)
			return
		}
		w.Header().Set("Set-Cookie", "stolen=1")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "hello %s cookie=%s authorization=%s origin=%s", r.URL.Path, r.Header.Get("Cookie"), r.Header.Get("Authorization"), r.Header.Get("Origin"))
	}))
	os.Exit(0)
}

func fixture(t *testing.T, mode string) (*Manager, *workbench.Store, Request, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	_ = os.Mkdir(source, 0700)
	git := func(dir string, args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return strings.TrimSpace(string(b))
	}
	git(source, "init", "--template=", "-b", "main")
	git(source, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	data := filepath.Join(root, "data")
	id := strings.Repeat("a", 32)
	parent := filepath.Join(data, "worktrees", id)
	_ = os.MkdirAll(parent, 0700)
	tree := filepath.Join(parent, "tree")
	branch := "kaiwu/task-" + id
	git(source, "worktree", "add", "-b", branch, tree)
	base := git(tree, "rev-parse", "HEAD")
	owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
	_ = os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
	s, e := workbench.Open(filepath.Join(data, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	_ = s.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: source}
		st.Tasks[id] = workbench.Task{ID: id, ProjectID: "p", ConversationID: "c", ExecutionID: "x", Status: "needs_confirmation"}
		st.Executions["x"] = workbench.Execution{ID: "x", TaskID: id, ConversationID: "c", Workspace: tree, Branch: branch, BaseSHA: base, Phase: "awaiting_review", FinishedAt: "done"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", TaskID: id, ProjectID: "p", RunState: "idle", SessionID: "original", SessionFile: "original.jsonl", Rounds: []workbench.Round{{ID: "r", Phase: "finished", FinishedAt: "done"}}}
		return nil
	})
	m := New(context.Background(), s, Config{DataDir: data})
	m.startupTimeout = 350 * time.Millisecond
	t.Cleanup(func() { m.Close() })
	if mode != "missing" {
		exe, _ := os.Executable()
		b, _ := json.Marshal(map[string]any{"version": 1, "command": []string{exe, "-test.run=TestHTTPHelper", "--", "preview-helper", mode, "{port}"}, "path": "/hello"})
		_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), b, 0600)
	}
	req := Request{ProjectID: "p", TaskID: id, RoundID: "r", OperationID: "start-one"}
	shown, e := m.Status(req)
	if e != nil {
		t.Fatal(e)
	}
	req.ManifestID = shown.ManifestID
	return m, s, req, tree
}

func TestRealHTTPStartOpenStopAndIsolation(t *testing.T) {
	m, s, req, _ := fixture(t, "http")
	v, e := m.Status(req)
	if e != nil || v.State != "stopped" || len(v.Command) == 0 {
		t.Fatal(v, e)
	}
	if _, e = m.Open(req, "login"); e == nil {
		t.Fatal("open started process")
	}
	v, e = m.Start(req)
	if e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	pid := m.entries[req.TaskID].cmd.Process.Pid
	if v, e = m.Start(req); e != nil || v.State != "running" || m.entries[req.TaskID].cmd.Process.Pid != pid {
		t.Fatal("duplicate start", v, e)
	}
	opened, e := m.Open(req, "login")
	if e != nil || !strings.HasPrefix(opened.URL, "/preview/") {
		t.Fatal(opened, e)
	}
	call := func(path, method string, valid bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://workbench.test"+path, nil)
		r.Header.Set("Cookie", "kaiwu_session=secret")
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Origin", "null")
		w := httptest.NewRecorder()
		m.Gateway(w, r, func(hash string) bool { return valid && hash == "login" })
		return w
	}
	w := call(opened.URL, "GET", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello /hello cookie= authorization= origin=") || w.Header().Get("Set-Cookie") != "" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || w.Header().Get("Access-Control-Allow-Origin") != "null" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	if w = call(prefix+"redirect", "GET", true); w.Code != 502 || w.Header().Get("Location") != "" {
		t.Fatal("external redirect", w.Code, w.Header())
	}
	if w = call(prefix+"local-redirect", "GET", true); w.Code != 302 || w.Header().Get("Location") != prefix+"hello" {
		t.Fatal("relative redirect", w.Code, w.Header())
	}
	for _, test := range []struct {
		method string
		valid  bool
		status int
	}{{"POST", true, 405}, {"GET", false, 401}, {"HEAD", true, 200}} {
		if w = call(opened.URL, test.method, test.valid); w.Code != test.status {
			t.Fatal(test, w.Code)
		}
	}
	m.mu.Lock()
	for _, token := range m.tokens {
		token.expires = time.Now().Add(-time.Second)
	}
	m.mu.Unlock()
	if w = call(opened.URL, "GET", true); w.Code != 401 {
		t.Fatal("expired token", w.Code)
	}
	req.OperationID = "stop-one"
	v, e = m.Stop(req)
	if e != nil || v.State != "stopped" {
		t.Fatal(v, e)
	}
	if !groupGone(pid) {
		t.Fatal("group survived stop")
	}
	if s.Snapshot().Tasks[req.TaskID].Status != "needs_confirmation" || s.Snapshot().Conversations["c"].SessionID != "original" {
		t.Fatal("preview changed result or session")
	}
}

func TestManifestAndAdmissionFailures(t *testing.T) {
	for _, mode := range []string{"missing", "bad", "old", "busy", "identity", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			m, s, req, tree := fixture(t, "missing")
			switch mode {
			case "bad":
				_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":1,"command":["echo","{port}"],"path":"//external","unknown":1}`), 0600)
			case "old":
				req.RoundID = "old"
			case "busy":
				_ = s.Update(func(st *workbench.State) error {
					c := st.Conversations["c"]
					c.RunState = "running"
					st.Conversations["c"] = c
					return nil
				})
			case "identity":
				_ = os.WriteFile(filepath.Join(filepath.Dir(tree), "owner.json"), []byte(`{}`), 0600)
			case "workspace":
				_ = s.Update(func(st *workbench.State) error {
					x := st.Executions["x"]
					x.Workspace = st.Projects["p"].Path
					st.Executions["x"] = x
					return nil
				})
			}
			if mode == "missing" {
				v, e := m.Status(req)
				if e != nil || v.State != "unavailable" {
					t.Fatal(v, e)
				}
			}
			v, e := m.Start(req)
			if e == nil {
				t.Fatal("invalid start accepted", v)
			}
			if len(m.entries) != 0 {
				t.Fatal("invalid start launched")
			}
		})
	}
}

func TestTimeoutLogBoundsWildcardAndClose(t *testing.T) {
	for _, mode := range []string{"timeout", "wildcard", "http"} {
		t.Run(mode, func(t *testing.T) {
			m, _, req, _ := fixture(t, mode)
			v, e := m.Start(req)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "http" {
				if v.State != "running" {
					t.Fatal(v)
				}
				pid := m.entries[req.TaskID].cmd.Process.Pid
				if e = m.Close(); e != nil || !groupGone(pid) {
					t.Fatal("close did not reap", e)
				}
			} else {
				if v.State != "failed" || len(v.Logs) > 64<<10 {
					t.Fatal(v.State, len(v.Logs))
				}
				if mode == "timeout" && len(v.Logs) == 0 {
					t.Fatal("lost diagnostics")
				}
			}
		})
	}
}

func TestForeignSocketOwnership(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if ownedPort(port, os.Getpid()+100000) == nil {
		t.Fatal("foreign listener accepted")
	}
}

func TestManifestChangeAndOperationBinding(t *testing.T) {
	m, _, req, tree := fixture(t, "http")
	if v, e := m.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	other := req
	other.RoundID = "different"
	if _, e := m.Start(other); e == nil {
		t.Fatal("operation replay rebound")
	}
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":1,"command":["bad","{port}"],"path":"/"}`), 0600)
	if v, e := m.Status(req); e == nil && v.State == "running" {
		t.Fatal("changed manifest shown running")
	}
}

func TestNaturalLeaderExitKeepsIdentityUntilDescendantsStopped(t *testing.T) {
	m, _, req, tree := fixture(t, "leader-exit")
	v, e := m.Start(req)
	if e != nil || v.State != "failed" {
		t.Fatal(v, e)
	}
	if _, e = os.Stat(filepath.Join(tree, "descendant-ready")); e != nil {
		t.Fatal("descendant did not run", e)
	}
	if _, e = os.Stat(filepath.Join(tree, "leader-reaped-before-descendant")); !os.IsNotExist(e) {
		t.Fatal("leader was reaped while live descendants still needed group signalling")
	}
	if !groupGone(m.entries[req.TaskID].cmd.Process.Pid) {
		t.Fatal("descendant group survived leader exit")
	}
	// Repeated cleanup of the retired entry must never send another PGID signal.
	req.OperationID = "stop-exited"
	if _, e = m.Stop(req); e != nil {
		t.Fatal(e)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestFailedLaunchRemainsVisibleUntilExplicitRetry(t *testing.T) {
	m, _, req, tree := fixture(t, "http")
	b, _ := json.Marshal(map[string]any{"version": 1, "command": []string{filepath.Join(tree, "missing-program"), "{port}"}, "path": "/"})
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), b, 0600)
	shown, e := m.Status(req)
	if e != nil {
		t.Fatal(e)
	}
	req.ManifestID = shown.ManifestID
	v, e := m.Start(req)
	if e != nil || v.State != "failed" {
		t.Fatal(v, e)
	}
	v, e = m.Status(req)
	if e != nil || v.State != "failed" {
		t.Fatal("refresh lost launch failure", v, e)
	}
}

func TestProcessExitReleasesLeaseWithoutClientPolling(t *testing.T) {
	m, _, req, _ := fixture(t, "http")
	v, e := m.Start(req)
	if e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	en := m.entries[req.TaskID]
	en.process.stop()
	end := time.Now().Add(2 * time.Second)
	for time.Now().Before(end) {
		l, e := acquireLock(en.tree)
		if e == nil {
			_ = l.Release(true)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("exited preview retained worktree lease until a client query")
}

func TestInheritedFlockSurvivesParentDescriptorClose(t *testing.T) {
	m, _, req, _ := fixture(t, "http")
	v, e := m.Start(req)
	if e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	en := m.entries[req.TaskID]
	// Duplicate our reference before closing the original; child fd 3 is then
	// the only holder of that open file description while we attempt the lock.
	fd, e := syscall.Dup(int(en.lease.File().Fd()))
	if e != nil {
		t.Fatal(e)
	}
	_ = en.lease.File().Close()
	_ = syscall.Close(fd)
	f, e := os.OpenFile(filepath.Join(filepath.Dir(en.tree), "execution.lock"), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
		t.Fatal("child did not inherit execution flock")
	}
	// Parent descriptor closure simulates a crashed host. Only this test's
	// still-owned process handle stops the child; production never kills a PID
	// loaded from the marker. The manually closed lease needs no second close.
	en.process.stop()
	<-en.done
	m.mu.Lock()
	en.lease = nil
	m.mu.Unlock()
}

func TestAnotherManagerCannotStartSameWorktree(t *testing.T) {
	m, s, req, _ := fixture(t, "http")
	v, e := m.Start(req)
	if e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	m2 := New(context.Background(), s, m.cfg)
	defer m2.Close()
	req.OperationID = "different-host"
	if _, e = m2.Start(req); e == nil {
		t.Fatal("second manager duplicated writer")
	}
	if len(m2.entries) != 0 {
		t.Fatal("second process launched")
	}
}

func TestInterruptedExecutionCannotPreviewFinishedRoundAsCurrent(t *testing.T) {
	m, s, req, _ := fixture(t, "http")
	_ = s.Update(func(st *workbench.State) error {
		x := st.Executions["x"]
		x.Phase = "interrupted"
		st.Executions[x.ID] = x
		return nil
	})
	if v, e := m.Start(req); e == nil && v.State == "running" {
		t.Fatal("stale finished round hid unfinished execution")
	}
}

func TestPreviewCapacityAndExitedSlotReuse(t *testing.T) {
	m, s, req, tree := fixture(t, "http")
	m.startupTimeout = 2 * time.Second
	manifest, e := os.ReadFile(filepath.Join(tree, "kaiwu-preview.json"))
	if e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	source := st.Projects["p"].Path
	base := st.Executions["x"].BaseSHA
	requests := []Request{req}
	for _, letter := range []string{"b", "c", "d", "e"} {
		id := strings.Repeat(letter, 32)
		parent := filepath.Join(m.cfg.DataDir, "worktrees", id)
		_ = os.MkdirAll(parent, 0700)
		tree := filepath.Join(parent, "tree")
		branch := "kaiwu/task-" + id
		if b, e := exec.Command("git", "-C", source, "worktree", "add", "-b", branch, tree).CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		owner, _ := json.Marshal(map[string]any{"TaskID": id, "Source": source, "Branch": branch, "BaseSHA": base, "Ready": true})
		_ = os.WriteFile(filepath.Join(parent, "owner.json"), owner, 0600)
		_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), manifest, 0600)
		_ = s.Update(func(st *workbench.State) error {
			st.Tasks[id] = workbench.Task{ID: id, ProjectID: "p", ConversationID: "conv-" + letter, ExecutionID: "exec-" + letter}
			st.Executions["exec-"+letter] = workbench.Execution{ID: "exec-" + letter, TaskID: id, ConversationID: "conv-" + letter, Workspace: tree, Branch: branch, BaseSHA: base, Phase: "awaiting_review", FinishedAt: "done"}
			st.Conversations["conv-"+letter] = workbench.Conversation{ID: "conv-" + letter, ProjectID: "p", TaskID: id, RunState: "idle", Rounds: []workbench.Round{{ID: "r", Phase: "finished", FinishedAt: "done"}}}
			return nil
		})
		requests = append(requests, Request{ProjectID: "p", TaskID: id, RoundID: "r", OperationID: "start-" + letter})
	}
	for i := range requests {
		shown, e := m.Status(requests[i])
		if e != nil {
			t.Fatal(e)
		}
		requests[i].ManifestID = shown.ManifestID
	}
	for _, r := range requests[:4] {
		if v, e := m.Start(r); e != nil || v.State != "running" {
			t.Fatal(v, e)
		}
	}
	if _, e := m.Start(requests[4]); e == nil {
		t.Fatal("fifth preview bypassed capacity")
	}
	m.entries[req.TaskID].process.stop()
	end := time.Now().Add(2 * time.Second)
	for time.Now().Before(end) {
		m.mu.Lock()
		released := m.entries[req.TaskID].lease == nil
		m.mu.Unlock()
		if released {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	requests[4].OperationID = "retry-after-exit"
	if v, e := m.Start(requests[4]); e != nil || v.State != "running" {
		t.Fatal("exited slot not reusable", v, e)
	}
}

func TestDisplayedManifestChangeRejectsWithoutSideEffectsThenAllowsRefresh(t *testing.T) {
	m, _, req, tree := fixture(t, "http")
	shown, e := m.Status(req)
	if e != nil || len(shown.ManifestID) != 64 {
		t.Fatal(shown, e)
	}
	var config manifest
	b, e := os.ReadFile(filepath.Join(tree, "kaiwu-preview.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &config); e != nil {
		t.Fatal(e)
	}
	config.Command[len(config.Command)-2] = "changed"
	b, _ = json.Marshal(config)
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), b, 0600)
	if _, e = m.Start(req); e == nil {
		t.Fatal("unshown command accepted")
	}
	missing := req
	missing.ManifestID = ""
	if _, e = m.Start(missing); e == nil {
		t.Fatal("missing displayed fingerprint accepted")
	}
	if len(m.entries) != 0 {
		t.Fatal("rejected approval changed process state")
	}
	for _, name := range []string{"approved-changed", "execution.lock", "process-owner.json"} {
		path := filepath.Join(tree, name)
		if name != "approved-changed" {
			path = filepath.Join(filepath.Dir(tree), name)
		}
		if _, e = os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("rejected approval caused side effect", name, e)
		}
	}
	fresh, e := m.Status(req)
	if e != nil || fresh.ManifestID == shown.ManifestID {
		t.Fatal("refresh did not expose new configuration", fresh, e)
	}
	req.ManifestID = fresh.ManifestID
	req.OperationID = "approve-changed"
	if v, e := m.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	if b, e := os.ReadFile(filepath.Join(tree, "approved-changed")); e != nil || string(b) != "explicitly approved" {
		t.Fatal("newly displayed command did not execute", e)
	}
	rebound := req
	rebound.ManifestID = shown.ManifestID
	if _, e = m.Start(rebound); e == nil {
		t.Fatal("operation approval rebound to different manifest")
	}
}

func TestReplayedStopCannotTerminateLaterExplicitStart(t *testing.T) {
	m, _, req, _ := fixture(t, "http")
	if v, e := m.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	stop := req
	stop.OperationID = "stop-first"
	if _, e := m.Stop(stop); e != nil {
		t.Fatal(e)
	}
	req.OperationID = "start-second"
	if v, e := m.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	if _, e := m.Stop(stop); e != nil {
		t.Fatal(e)
	}
	if v, e := m.Status(req); e != nil || v.State != "running" {
		t.Fatal("replayed stop killed newer process", v, e)
	}
}
