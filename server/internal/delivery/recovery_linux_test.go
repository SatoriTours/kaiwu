//go:build linux

package delivery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// The crash tests run a real child process, let it reach one durable
// interruption window and SIGKILL it there. The parent then reopens the same
// SQLite store and the same real bare remote, so nothing about the crash is
// simulated: only the file system, the store and the remote survive.

const (
	crashRootEnv  = "KAIWU_RECOVERY_ROOT"
	crashPointEnv = "KAIWU_RECOVERY_POINT"
)

// recoveryWorld is one throwaway project with a real bare remote.
type recoveryWorld struct {
	root     string
	store    *workbench.Store
	project  string
	task     string
	delivery string
	round    string
	source   string
	tree     string
	base     string
	remote   string
	pushes   string
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s", args, b)
	}
	return strings.TrimSpace(string(b))
}

// buildRecoveryWorld creates the repository set, the store and one delivery
// cycle in a fixed directory so both the child and the parent can use it.
func buildRecoveryWorld(t *testing.T, root string) *recoveryWorld {
	t.Helper()
	w := &recoveryWorld{
		root:   root,
		source: filepath.Join(root, "project"),
		remote: filepath.Join(root, "remote.git"),
		pushes: filepath.Join(root, "pushes.log"),
	}
	if e := os.MkdirAll(w.source, 0700); e != nil {
		t.Fatal(e)
	}
	gitRun(t, w.source, "init", "--initial-branch=main", "--template=")
	if e := os.WriteFile(filepath.Join(w.source, "base.txt"), []byte("base\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitRun(t, w.source, "add", ".")
	gitRun(t, w.source, "commit", "-m", "base")
	w.base = gitRun(t, w.source, "rev-parse", "refs/heads/main")

	if e := os.MkdirAll(w.remote, 0700); e != nil {
		t.Fatal(e)
	}
	gitRun(t, w.remote, "init", "--bare", "--initial-branch=main")
	gitRun(t, w.source, "remote", "add", "origin", w.remote)
	gitRun(t, w.source, "push", w.remote, w.base+":refs/heads/main")

	w.task = strings.Repeat("7", 32)
	parent := filepath.Join(root, "worktrees", w.task)
	if e := os.MkdirAll(parent, 0700); e != nil {
		t.Fatal(e)
	}
	w.tree = filepath.Join(parent, "tree")
	gitRun(t, w.source, "worktree", "add", "-b", "kaiwu/task-"+w.task, w.tree, w.base)
	if e := os.WriteFile(filepath.Join(w.tree, "feature.txt"), []byte("feature\n"), 0600); e != nil {
		t.Fatal(e)
	}
	owner := fmt.Sprintf(`{"TaskID":%q,"Source":%q,"Branch":"kaiwu/task-%s","BaseSHA":%q,"Ready":true}`, w.task, w.source, w.task, w.base)
	if e := os.WriteFile(filepath.Join(parent, "owner.json"), []byte(owner), 0600); e != nil {
		t.Fatal(e)
	}

	store, e := workbench.Open(filepath.Join(root, "state.db"), workbench.Profile{Provider: "fixture", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	w.store = store
	if e = store.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: w.source, DefaultBranch: "main", MainSeen: true, CommitName: "Fixture", CommitEmail: "fixture@example.invalid"}
		st.Tasks[w.task] = workbench.Task{ID: w.task, ProjectID: "p", ConversationID: "c", Status: "idle", Title: "崩溃恢复", Request: "fixture"}
		if st.Conversations == nil {
			st.Conversations = map[string]workbench.Conversation{}
		}
		st.Conversations["c"] = workbench.Conversation{ID: "c", TaskID: w.task, RunState: "idle"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	p, e := store.DeliveryPolicy("p")
	if e != nil {
		t.Fatal(e)
	}
	p.Enabled = true
	p.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	p.Checks = []workbench.DeliveryCheck{{ID: "ok", Argv: []string{"true"}, Dir: ".", TimeoutSeconds: 300}}
	p.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	p.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2}}
	p.GitCredentialRef = "server_github_https"
	p.APICredentialRef = "server_github_api"
	p.AuthorizationDigest = "explicit-scope"
	p.Actor = "local-login"
	p.AuthorizedAt = time.Now().UTC()
	if _, e = store.SaveDeliveryPolicy("enable-recovery", 0, p); e != nil {
		t.Fatal(e)
	}
	r, e := store.Command(workbench.Command{OperationID: "start-recovery", Revision: store.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: w.task})
	if e != nil {
		t.Fatal(e)
	}
	w.delivery, w.round = r.DeliveryID, r.CreatedID
	snap := store.Snapshot()
	x := snap.Executions[snap.Tasks[w.task].ExecutionID]
	if e = store.Update(func(st *workbench.State) error {
		x.Workspace, x.Branch, x.BaseSHA = w.tree, "kaiwu/task-"+w.task, w.base
		st.Executions[x.ID] = x
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return w
}

// recoveryTransport routes the frozen remote URL onto the local bare repo and
// records every push so both processes can count real writes.
func recoveryTransport(w *recoveryWorld) gitops.RemoteTransportFunc {
	return func(ctx context.Context, dir string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
		a := append([]string{}, args...)
		for i := range a {
			if strings.Contains(a[i], "github.com") {
				a[i] = w.remote
			}
		}
		if a[0] == "push" {
			f, e := os.OpenFile(w.pushes, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if e == nil {
				fmt.Fprintln(f, a[len(a)-1])
				_ = f.Close()
			}
		}
		return gitops.Run(ctx, dir, gitops.LeaseTrack(lease), a...)
	}
}

func recoveryPushes(t *testing.T, w *recoveryWorld) []string {
	t.Helper()
	b, e := os.ReadFile(w.pushes)
	if e != nil {
		return nil
	}
	return strings.FieldsFunc(string(b), func(r rune) bool { return r == '\n' })
}

func countRef(pushes []string, ref string) int {
	n := 0
	for _, p := range pushes {
		if strings.HasSuffix(p, ":"+ref) || strings.HasSuffix(p, " "+ref) {
			n++
		}
	}
	return n
}

// settleWorldRound marks the cycle's round finished the way the agent does.
func settleWorldRound(t *testing.T, w *recoveryWorld) {
	t.Helper()
	if e := w.store.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.RunState = "idle"
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" {
				c.Messages[i].Delivery = "accepted"
			}
		}
		st.Conversations["c"] = c
		workbench.UpdateRound(st, "c", w.round, func(r *workbench.Round) {
			r.Phase = "finished"
			r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

// wallClock is the production-shaped clock for the real-process tests.
type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// crashChildWorld is the child process: it runs the real machine with the real
// bare remote, arms one interruption point and SIGKILLs itself there.
func crashChildWorld(t *testing.T, root, point string) {
	w := buildRecoveryWorld(t, root)
	defer func() { _ = w.store.Close() }()
	agent := &fakeAgentPort{lease: &stubLease{accept: true}, claimStore: w.store}
	ci := &ciState{}
	cfg := Config{
		Agent:   agent,
		Actions: ci.client(t, w.store, w.delivery, w.task, "token"),
		Remote:  gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: recoveryTransport(w)}),
		Clock:   wallClock{},
		DataDir: root,
	}
	mgr, e := New(context.Background(), w.store, cfg)
	if e != nil {
		t.Fatal(e)
	}
	setCrashPoint(point)
	settleWorldRound(t, w)
	// The human gate: accept the candidate so the cycle may integrate.
	acceptBy := time.Now().Add(90 * time.Second)
	for time.Now().Before(acceptBy) {
		d, e := w.store.Delivery(w.delivery)
		if e != nil {
			t.Fatal(e)
		}
		if d.Phase == "awaiting_acceptance" {
			if _, e := mgr.Accept(context.Background(), "accept-child", w.delivery, d.AcceptanceToken, d.Version); e != nil {
				t.Fatal(e)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Wait until the requested window is durably persisted, then die.
	window := func() bool {
		d, e := w.store.Delivery(w.delivery)
		if e != nil {
			t.Fatal(e)
		}
		if point == "after-main-receipt" {
			return d.Phase == "main_ci"
		}
		page, e := w.store.DeliverySteps(w.delivery, 0, 100)
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range page.Items {
			if s.Kind == "push_main" && s.Phase == "running" {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && !window() {
		time.Sleep(5 * time.Millisecond)
	}
	if !window() {
		d, _ := w.store.Delivery(w.delivery)
		page, _ := w.store.DeliverySteps(w.delivery, 0, 100)
		t.Fatalf("child never reached the crash window: phase=%s block=%s/%s steps=%d pushes=%v", d.Phase, d.BlockCode, d.BlockReason, len(page.Items), recoveryPushes(t, w))
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {}
}

// TestDeliveryRecoveryCrashChild is the helper entry point; it only runs when
// the parent set the shared root.
func TestDeliveryRecoveryCrashChild(t *testing.T) {
	root := os.Getenv(crashRootEnv)
	if root == "" {
		t.Skip("crash helper only runs under the parent test")
	}
	crashChildWorld(t, root, os.Getenv(crashPointEnv))
}

// TestDeliveryRecoveryCrashAndRestart kills a real process at each
// interruption point and proves the reopened service recovers from records:
// a landed push is never repeated, an unlanded intent is retried once, a
// receipt-complete window needs nothing, and the CI window and queue slot
// survive.
func TestDeliveryRecoveryCrashAndRestart(t *testing.T) {
	for _, point := range []string{"after-main-intent", "after-main-push", "after-main-receipt"} {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=TestDeliveryRecoveryCrashChild", "-test.v")
			cmd.Env = append(os.Environ(), crashRootEnv+"="+root, crashPointEnv+"="+point)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("child did not crash: %s", out)
			}
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() >= 0 {
				t.Fatalf("child did not die by signal: %v (%s)", err, out)
			}

			w := buildRecoveryWorldFrom(t, root)
			defer func() { _ = w.store.Close() }()
			agent := &fakeAgentPort{lease: &stubLease{accept: true}, claimStore: w.store}
			ci := &ciState{}
			m, e := New(context.Background(), w.store, Config{
				Agent:   agent,
				Actions: ci.client(t, w.store, w.delivery, w.task, "token"),
				Remote:  gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: recoveryTransport(w)}),
				Clock:   wallClock{},
				DataDir: root,
			})
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = m.Close() }()

			before := recoveryPushes(t, w)
			if e := m.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			// The logical integration slot survives the crash.
			if ok, e := w.store.AcquireDeliveryTurn(w.delivery); e != nil || !ok {
				t.Fatalf("queue slot lost across the crash: %v %v", ok, e)
			}
			waitWorldPhase(t, w, "succeeded")
			after := recoveryPushes(t, w)
			mainRef := "refs/heads/main"
			switch point {
			case "after-main-push":
				if countRef(after, mainRef) != countRef(before, mainRef) {
					t.Fatalf("recovery repeated the landed main push: %v -> %v", before, after)
				}
			case "after-main-intent":
				if countRef(after, mainRef) != countRef(before, mainRef)+1 {
					t.Fatalf("recovery did not retry exactly once: %v -> %v", before, after)
				}
			case "after-main-receipt":
				if countRef(after, mainRef) != countRef(before, mainRef) {
					t.Fatalf("recovery repeated a completed push: %v -> %v", before, after)
				}
			}
			if got := gitRun(t, w.remote, "rev-parse", "refs/heads/main"); got == w.base {
				t.Fatal("main was never published to the bare remote")
			}
		})
	}
}

// buildRecoveryWorldFrom reopens a world that a previous process created.
func buildRecoveryWorldFrom(t *testing.T, root string) *recoveryWorld {
	t.Helper()
	w := &recoveryWorld{
		root:   root,
		source: filepath.Join(root, "project"),
		remote: filepath.Join(root, "remote.git"),
		pushes: filepath.Join(root, "pushes.log"),
	}
	store, e := workbench.Open(filepath.Join(root, "state.db"), workbench.Profile{Provider: "fixture", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	w.store = store
	snap := store.Snapshot()
	for id, t2 := range snap.Tasks {
		w.task = id
		w.tree = snap.Executions[t2.ExecutionID].Workspace
		w.base = snap.Executions[t2.ExecutionID].BaseSHA
	}
	for _, s := range snap.Deliveries {
		w.delivery = s.ID
	}
	if w.delivery == "" || w.task == "" {
		t.Fatalf("reopened world is missing the cycle: %+v", snap.Deliveries)
	}
	return w
}

func waitWorldPhase(t *testing.T, w *recoveryWorld, phase string) workbench.Delivery {
	t.Helper()
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		d, e := w.store.Delivery(w.delivery)
		if e != nil {
			t.Fatal(e)
		}
		if d.Phase == phase {
			return d
		}
		time.Sleep(10 * time.Millisecond)
	}
	d, _ := w.store.Delivery(w.delivery)
	t.Fatalf("delivery stuck in %q, want %q (block=%q %s)", d.Phase, phase, d.BlockCode, d.BlockReason)
	return d
}
