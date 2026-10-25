// Isolated browser fixture. Never linked into the production server: all
// remote writes target a temporary bare repository, and all Actions requests
// are served by a local RoundTripper. No personal configuration or API calls.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/delivery"
	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/webapi"
	"github.com/SatoriTours/kaiwu/server/internal/webui"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const fixtureToken = "kaiwu-delivery-fixture-token-00000000"

type pushRecord struct {
	SHA string `json:"sha"`
	Ref string `json:"ref"`
	TaskID string `json:"taskId"`
}
type runRecord struct{ SHA, Branch, Conclusion string }

type fixture struct {
	store                            *workbench.Store
	agent                            *agent.Manager
	worker                           *delivery.Manager
	handler                          http.Handler
	root, project, remote, projectID string
	mu                               sync.Mutex
	pushes                           []pushRecord
	queries                          []string
	holdMain                         bool
	runs                             map[int64]runRecord
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("fixture git %v: %w: %s", args, err, b)
	}
	return strings.TrimSpace(string(b)), nil
}

func newFixture(ctx context.Context, root, program, host string) (f *fixture, err error) {
	f = &fixture{root: root, project: filepath.Join(root, "project"), remote: filepath.Join(root, "remote.git"), pushes: []pushRecord{}, queries: []string{}, runs: map[int64]runRecord{}}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()
	if err = os.MkdirAll(f.project, 0700); err != nil {
		return
	}
	for _, args := range [][]string{{"init", "--initial-branch=main", "--template="}} {
		if _, err = git(ctx, f.project, args...); err != nil {
			return
		}
	}
	if err = os.WriteFile(filepath.Join(f.project, "base.txt"), []byte("base\n"), 0600); err != nil {
		return
	}
	for _, args := range [][]string{{"add", "base.txt"}, {"commit", "-m", "base"}, {"init", "--bare", "--initial-branch=main", f.remote}, {"push", f.remote, "main:refs/heads/main"}, {"remote", "add", "origin", "https://github.com/owner/repo.git"}} {
		if _, err = git(ctx, f.project, args...); err != nil {
			return
		}
	}
	data := filepath.Join(root, "data")
	if err = os.MkdirAll(data, 0700); err != nil {
		return
	}
	f.store, err = workbench.Open(filepath.Join(data, "state.db"), workbench.Profile{Provider: "fixture", Model: "isolated-test"})
	if err != nil {
		return
	}
	var r workbench.Receipt
	r, err = f.store.Command(workbench.Command{OperationID: "fixture-project", Revision: f.store.Snapshot().Revision, Type: "add_project", Name: "自动交付测试项目", Path: f.project})
	if err != nil {
		return
	}
	f.projectID = r.CreatedID
	err = f.store.Update(func(st *workbench.State) error {
		p := st.Projects[f.projectID]
		p.DefaultBranch = "main"
		p.MainSeen = true
		p.CommitName = "Fixture"
		p.CommitEmail = "fixture@example.invalid"
		st.Projects[p.ID] = p
		return nil
	})
	if err != nil {
		return
	}
	f.agent = agent.New(ctx, f.store, agent.Config{Program: program, DataDir: data, PiAgentDir: filepath.Join(root, "no-personal-pi")})
	var actions *githubactions.Client
	actions, err = githubactions.NewClient(githubactions.ClientConfig{HTTP: &http.Client{Transport: f}, Token: func(context.Context, string) (string, error) { return "synthetic-fixture-token", nil }})
	if err != nil {
		return
	}
	remote := gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(f.remoteCommand)})
	f.worker, err = delivery.New(ctx, f.store, delivery.Config{Agent: f.agent, Actions: actions, Remote: remote, Clock: delivery.SystemClock{}, DataDir: data})
	if err != nil {
		return
	}
	if err = f.worker.Recover(ctx); err != nil {
		return
	}
	catalog := &pimodel.Catalog{Program: program, DataDir: filepath.Join(root, "probes"), SourceDir: filepath.Join(root, "no-personal-pi")}
	f.handler = webapi.New(webapi.Config{Context: ctx, Host: host, Token: fixtureToken, Assets: webui.Files(), AgentAvailable: true, Git: f.agent, Delivery: f.worker, Models: catalog}, f.store, f.agent)
	return
}

func (f *fixture) Close() {
	if h, ok := f.handler.(interface{ Close() error }); ok {
		_ = h.Close()
	}
	if f.worker != nil {
		_ = f.worker.Close()
	}
	if f.agent != nil {
		_ = f.agent.Close()
	}
	if f.store != nil {
		_ = f.store.Close()
	}
}

func (f *fixture) remoteCommand(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
	a := append([]string{}, args...)
	for i := range a {
		if strings.Contains(a[i], "github.com/") {
			a[i] = f.remote
		}
	}
	// This transport spawns real Git against an isolated bare remote. Attribute
	// its actual child to the write lease, just like the production transport.
	out, err := gitops.Run(ctx, dir, gitops.LeaseTrack(lease), a...)
	if err == nil && len(a) > 0 && a[0] == "push" {
		sha, ref, _ := strings.Cut(a[len(a)-1], ":")
		f.mu.Lock()
		f.pushes = append(f.pushes, pushRecord{SHA:sha, Ref:ref, TaskID:strings.TrimPrefix(target.TaskRef,"refs/heads/kaiwu/task-")})
		f.mu.Unlock()
	}
	return out, err
}

func response(status int, body any) *http.Response {
	b, _ := json.Marshal(body)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b)))}
}

// RoundTrip accepts only the exact synthetic GitHub repository. A run exists
// only after the real temporary remote contains the requested SHA/ref.
func (f *fixture) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "api.github.com" {
		return nil, fmt.Errorf("fixture refused non-API host")
	}
	path := req.URL.Path
	f.mu.Lock()
	f.queries = append(f.queries, req.URL.RequestURI())
	pushes := append([]pushRecord{}, f.pushes...)
	hold := f.holdMain
	f.mu.Unlock()
	switch {
	case path == "/repos/owner/repo":
		return response(200, map[string]any{"id": 42, "full_name": "owner/repo", "default_branch": "main"}), nil
	case path == "/repos/owner/repo/actions/workflows":
		return response(200, map[string]any{"total_count": 2, "workflows": []any{map[string]any{"id": 1, "state": "active", "path": ".github/workflows/task.yml", "name": "task"}, map[string]any{"id": 2, "state": "active", "path": ".github/workflows/main.yml", "name": "main"}}}), nil
	case strings.HasSuffix(path, "/runs") && strings.Contains(path, "/workflows/"):
		q := req.URL.Query()
		sha, branch := q.Get("head_sha"), q.Get("branch")
		rows := []any{}
		actual, e := git(req.Context(), f.remote, "rev-parse", "refs/heads/"+branch)
		if e == nil && actual == sha && sha != "" {
			conclusion, status := "success", "completed"
			contents, _ := git(req.Context(), f.remote, "show", sha+":kaiwu-fixture-result.txt")
			// Scenario comes from the user's synthetic task request, not from
			// fabricated product status; repair output may legitimately omit it.
			st := f.store.Snapshot()
			scenarioTaskID := strings.TrimPrefix(branch,"kaiwu/task-")
			for taskID, d := range st.Deliveries {
				if d.CandidateSHA == sha {
					contents += st.Tasks[taskID].Request
					scenarioTaskID = taskID
				}
			}
			count := 0
			for _, p := range pushes {
				if p.Ref == "refs/heads/"+branch && (branch!="main" || p.TaskID==scenarioTaskID) {
					count++
				}
			}
			if strings.Contains(contents, "[fail-always]") || (strings.Contains(contents, "[fail-once]") && count == 1) {
				conclusion = "failure"
			}
			if branch == "main" {
				// Task-failure scenarios only fail the task gate; the repaired main gate
				// is evaluated independently and cannot borrow the task's success.
				conclusion = "success"
				if strings.Contains(contents, "[main-fail]") && count == 1 {
					conclusion = "failure"
				}
				if hold {
					status = "in_progress"
					conclusion = ""
				}
			}
			hash := sha256.Sum256([]byte(branch + "@" + sha))
			run := int64(binary.BigEndian.Uint32(hash[:4])) + 1000
			f.mu.Lock()
			f.runs[run] = runRecord{SHA: sha, Branch: branch, Conclusion: conclusion}
			f.mu.Unlock()
			wf := int64(1)
			if branch == "main" {
				wf = 2
			}
			rows = append(rows, map[string]any{"id": run, "workflow_id": wf, "run_attempt": 1, "head_branch": branch, "head_sha": sha, "event": "push", "status": status, "conclusion": conclusion, "created_at": time.Now().UTC().Format(time.RFC3339), "html_url": fmt.Sprintf("https://github.com/owner/repo/actions/runs/%d", run)})
		}
		return response(200, map[string]any{"total_count": len(rows), "workflow_runs": rows}), nil
	case strings.Contains(path, "/attempts/") && strings.HasSuffix(path, "/jobs"):
		tail := strings.Split(strings.TrimPrefix(path, "/repos/owner/repo/actions/runs/"), "/")
		id, _ := strconv.ParseInt(tail[0], 10, 64)
		f.mu.Lock()
		run, found := f.runs[id]
		f.mu.Unlock()
		if !found {
			return response(404, map[string]any{}), nil
		}
		return response(200, map[string]any{"total_count": 1, "jobs": []any{map[string]any{"id": 6001, "run_id": id, "run_attempt": 1, "head_sha": run.SHA, "head_branch": run.Branch, "name": "test", "status": "completed", "conclusion": run.Conclusion, "steps": []any{map[string]any{"name": "test", "status": "completed", "conclusion": run.Conclusion}}}}}), nil
	case strings.HasSuffix(path, "/logs"):
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("feature.test: assertion failed: expected fixed result; repair the source code"))}, nil
	}
	return response(404, map[string]any{}), nil
}

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Test controls change the fake external Actions service only. They cannot
	// write task/delivery state or manufacture success in the product Store.
	if r.URL.Path == "/healthz" {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/__fixture/") {
		if r.Header.Get("X-Fixture-Token") != fixtureToken {
			http.Error(w, "fixture auth required", 401)
			return
		}
		if r.URL.Path == "/__fixture/ci" && r.Method == "POST" {
			var input struct {
				HoldMain bool `json:"holdMain"`
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			f.mu.Lock()
			f.holdMain = input.HoldMain
			f.mu.Unlock()
			f.worker.Wake()
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/__fixture/evidence" && r.Method == "GET" {
			f.mu.Lock()
			p := append([]pushRecord{}, f.pushes...)
			q := append([]string{}, f.queries...)
			f.mu.Unlock()
			refs, err := git(r.Context(), f.remote, "show-ref")
			if err != nil {
				http.Error(w, "remote unreadable", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"pushes": p, "actionsQueries": q, "refs": refs, "projectId": f.projectID, "path": f.project})
			return
		}
		http.NotFound(w, r)
		return
	}
	f.handler.ServeHTTP(w, r)
}

func main() {
	root := flag.String("root", "", "temporary fixture root")
	program := flag.String("pi", "", "isolated fake pi executable")
	flag.Parse()
	if *root == "" || *program == "" {
		panic("fixture requires explicit temporary root and fake pi")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ln, err := net.Listen("tcp", "127.0.0.1:17332")
	if err != nil {
		panic(err)
	}
	f, err := newFixture(ctx, *root, *program, ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		panic(err)
	}
	defer f.Close()
	server := &http.Server{Handler: f, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.Serve(ln); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
