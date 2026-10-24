package delivery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// fakeClock makes scheduling deadlines deterministic: no real sleeps.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	arms []chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.arms = append(c.arms, ch)
	return ch
}

// advance moves the clock and releases every pending After channel so the
// scheduler loop re-evaluates persisted deadlines.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	arms := c.arms
	c.arms = nil
	c.mu.Unlock()
	now := c.Now()
	for _, ch := range arms {
		ch <- now
	}
}

// fakeAgentPort records sends and hands out a stub lease, mirroring the
// narrow AgentPort the delivery worker is allowed to use. When claimStore is
// set, DispatchRepair performs the real store claim exactly like the
// Task-8 agent manager would, so budget charging stays real.
type fakeAgentPort struct {
	mu          sync.Mutex
	sends       [][2]string
	scopes      []string
	lease       *stubLease
	repairs     []string
	claimStore  *workbench.Store
	refuseLease bool
}

func (f *fakeAgentPort) Send(conversationID, messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, [2]string{conversationID, messageID})
	return nil
}

func (f *fakeAgentPort) DispatchRepair(_ context.Context, deliveryID, jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repairs = append(f.repairs, deliveryID+" "+jobID)
	if f.claimStore != nil {
		_, _, err := f.claimStore.ClaimDeliveryRepair(jobID)
		return err
	}
	return nil
}

func (f *fakeAgentPort) AcquireDeliveryLease(_ context.Context, _, scope string) (agent.DeliveryLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = append(f.scopes, scope)
	if f.refuseLease {
		return nil, fmt.Errorf("fixture 租约拒绝")
	}
	return f.lease, nil
}

func (f *fakeAgentPort) sent() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string{}, f.sends...)
}

func (f *fakeAgentPort) leases() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.scopes...)
}

func (f *fakeAgentPort) repairsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.repairs)
}

// remoteState tracks pushes and serves consistent remote observation in
// place of any real Git transport: no real git process ever runs.
type remoteState struct {
	mu      sync.Mutex
	pushes  []string
	refused bool
}

func (r *remoteState) transport(store *workbench.Store, taskID string) gitops.RemoteTransportFunc {
	return func(_ context.Context, _ string, target gitops.RemoteTarget, args []string, _ string, lease gitops.ProcessLease) (string, error) {
		// This transport is an in-memory remote: it spawns nothing, so the lease
		// may be told there is no process instead of guessing later.
		if f, ok := lease.(interface{ NoProcess() }); ok {
			f.NoProcess()
		}
		if len(args) == 0 {
			return "", fmt.Errorf("no args")
		}
		last := args[len(args)-1]
		switch args[0] {
		case "push":
			sha, ref, ok := strings.Cut(last, ":")
			if !ok {
				return "", fmt.Errorf("push spec %q", last)
			}
			r.mu.Lock()
			r.pushes = append(r.pushes, sha+" "+ref)
			refused := r.refused
			r.mu.Unlock()
			if refused {
				return "", fmt.Errorf("push denied by test")
			}
			return "", nil
		case "ls-remote":
			// Only a ref actually written by a confirmed push answers; the
			// newest push for that ref is the remote truth.
			r.mu.Lock()
			answer := ""
			for _, p := range r.pushes {
				sha, ref, _ := strings.Cut(p, " ")
				if ref == last {
					answer = sha + "\t" + ref + "\n"
				}
			}
			r.mu.Unlock()
			if answer != "" {
				return answer, nil
			}
			// The authorized main branch answers with the cycle's recorded
			// remote main until a real main push lands.
			if last == "refs/heads/"+target.MainRef {
				summary := store.Snapshot().Deliveries[taskID]
				if summary.RemoteMainSHA != "" {
					return summary.RemoteMainSHA + "\t" + last + "\n", nil
				}
				if summary.LocalMainSHA != "" {
					return summary.LocalMainSHA + "\t" + last + "\n", nil
				}
			}
			// Missing ref: git ls-remote --exit-code exits 2.
			return "", fakeExitTwo{}
		default:
			return "", fmt.Errorf("unexpected remote command %q", args[0])
		}
	}
}

// fakeExitTwo mirrors a git exit status 2 from ls-remote --exit-code.
type fakeExitTwo struct{}

func (fakeExitTwo) Error() string { return "remote ref absent (test)" }

func (r *remoteState) pushCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pushes)
}

// inject records an external writer's push so recovery can observe a remote
// that moved outside this service.
func (r *remoteState) inject(sha, ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushes = append(r.pushes, sha+" "+ref)
}

func (r *remoteState) pushedRefs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.pushes...)
}

func (r *remoteState) pushAt(k int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k < len(r.pushes) {
		return r.pushes[k]
	}
	return ""
}

// ciState mirrors GitHub Actions: one newest run per workflow query, whose
// SHA always follows the delivery's frozen candidate.
type ciState struct {
	mu        sync.Mutex
	otherSHA  bool   // serve a run row of a different SHA (wrong run)
	failure   bool   // run completes with conclusion failure
	inFlight  bool   // run stays in progress (never completes)
	missing   bool   // no run at all (waiting, no record)
	cancelled bool   // run completes with conclusion cancelled
	jobFailed bool   // jobs endpoint serves a failed job (6001)
	logBody   string // served at /repos/owner/repo/actions/jobs/6001/logs
	mainFail  bool   // the main-branch gate run completes with failure
	seen      []string
}

func (c *ciState) queries() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.seen...)
}

func (c *ciState) note(q string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, q)
}

func (c *ciState) setMainFailed(failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mainFail = failed
}

func (c *ciState) jobLogs() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logBody
}

func (c *ciState) setFailedJob(jobFailed bool, logBody string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jobFailed, c.logBody = jobFailed, logBody
}

func (c *ciState) client(t *testing.T, store *workbench.Store, deliveryID, taskID, actorToken string) *githubactions.Client {
	t.Helper()
	client, e := githubactions.NewClient(githubactions.ClientConfig{
		HTTP:  &http.Client{Transport: c.transport(store, deliveryID, taskID)},
		Token: func(context.Context, string) (string, error) { return actorToken, nil },
	})
	if e != nil {
		t.Fatal(e)
	}
	return client
}

func (c *ciState) transport(store *workbench.Store, deliveryID, taskID string) http.RoundTripper {
	return roundTrip(func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		if strings.Contains(path, "github.com/") {
			return nil, fmt.Errorf("external host reached by test")
		}
		switch {
		case path == "/repos/owner/repo":
			return respond(200, nil, `{"id":42,"full_name":"owner/repo","default_branch":"main"}`), nil
		case path == "/repos/owner/repo/actions/workflows":
			return respond(200, nil, `{"total_count":2,"workflows":[`+
				`{"id":1,"state":"active","path":".github/workflows/task.yml","name":"task"},`+
				`{"id":2,"state":"active","path":".github/workflows/main.yml","name":"main"}]}`), nil
		case path == "/repos/owner/repo/actions/workflows/1/runs",
			path == "/repos/owner/repo/actions/workflows/2/runs":
			return c.runsPage(store, taskID, req), nil
		case strings.HasPrefix(path, "/repos/owner/repo/actions/runs/") &&
			strings.Contains(path, "/attempts/"):
			return respond(200, nil, c.jobsBody(runIDFromPath(path))), nil
		case strings.HasPrefix(path, "/repos/owner/repo/actions/jobs/") &&
			strings.HasSuffix(path, "/logs"):
			return respond(200, map[string]string{"Content-Type": "text/plain"}, c.jobLogs()), nil
		}
		return respond(404, nil, `{}`), nil
	})
}

func (c *ciState) runsPage(store *workbench.Store, taskID string, req *http.Request) *http.Response {
	c.note(req.URL.RawQuery)
	c.mu.Lock()
	other, failure, inFlight, missing, cancelled, mainFail := c.otherSHA, c.failure, c.inFlight, c.missing, c.cancelled, c.mainFail
	c.mu.Unlock()
	if missing {
		return respond(200, nil, `{"total_count":0,"workflow_runs":[]}`)
	}
	query := req.URL.Query()
	headSHA := query.Get("head_sha")
	branch := query.Get("branch")
	if branch == "main" {
		// The main gate counts only runs of the exact integrated main SHA.
		if headSHA == "" {
			return respond(200, nil, `{"total_count":0,"workflow_runs":[]}`)
		}
		status, conclusion := "completed", "success"
		if mainFail {
			conclusion = "failure"
		}
		if inFlight {
			status, conclusion = "in_progress", ""
		}
		row := fmt.Sprintf(`{"id":901,"run_attempt":1,"head_branch":%q,"head_sha":%q,"event":"push",`+
			`"status":%q,"conclusion":%q,"created_at":"2026-10-17T12:30:00Z",`+
			`"html_url":"https://github.com/owner/repo/actions/runs/901"}`, branch, headSHA, status, conclusion)
		return respond(200, nil, `{"total_count":1,"workflow_runs":[`+row+`]}`)
	}
	// The gate always observes the cycle the task map projects right now:
	// cycle summaries are keyed by task and replaced when a new cycle opens.
	task := taskID
	if strings.HasPrefix(branch, "kaiwu/task-") {
		task = strings.TrimPrefix(branch, "kaiwu/task-")
	}
	summary := store.Snapshot().Deliveries[task]
	if summary.ID == "" || summary.CandidateSHA == "" {
		return respond(200, nil, `{"total_count":0,"workflow_runs":[]}`)
	}
	sha := summary.CandidateSHA
	if other {
		sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}
	status, conclusion := "completed", "success"
	switch {
	case failure:
		conclusion = "failure"
	case cancelled:
		conclusion = "cancelled"
	}
	if inFlight {
		status, conclusion = "in_progress", ""
	}
	row := fmt.Sprintf(`{"id":900,"run_attempt":1,"head_branch":%q,"head_sha":%q,"event":"push",`+
		`"status":%q,"conclusion":%q,"created_at":"2026-10-17T12:30:00Z",`+
		`"html_url":"https://github.com/owner/repo/actions/runs/900"}`, branch, sha, status, conclusion)
	return respond(200, nil, `{"total_count":1,"workflow_runs":[`+row+`]}`)
}

func (c *ciState) jobsBody(runID string) string {
	c.mu.Lock()
	taskFailed, mainFail, jobFailed := c.failure, c.mainFail, c.jobFailed
	c.mu.Unlock()
	// A task run can only fail when the task gate fails; the main run only
	// when the main gate fails. Serving a failed job to a passing gate would
	// misreport the gate itself.
	failed := taskFailed
	if runID == "901" {
		failed = mainFail
	}
	if failed && jobFailed {
		return `{"total_count":1,"jobs":[` +
			`{"id":6001,"run_id":900,"run_attempt":1,"name":"test","status":"completed",` +
			`"conclusion":"failure","html_url":"https://github.com/owner/repo/actions/runs/900/job/6001"}]}`
	}
	return `{"total_count":1,"jobs":[` +
		`{"id":5001,"run_id":900,"run_attempt":1,"name":"test","status":"completed",` +
		`"conclusion":"success","html_url":"https://github.com/owner/repo/actions/runs/900/job/5001"}]}`
}

func runIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "runs" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func roundTrip(fn func(*http.Request) (*http.Response, error)) http.RoundTripper {
	return rtFunc(fn)
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func respond(status int, header map[string]string, body string) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "application/json")
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func (c *ciState) setCancelled(cancelled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelled = cancelled
}

func (c *ciState) setOpen(otherSHA, failure, inFlight, missing bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.otherSHA, c.failure, c.inFlight, c.missing = otherSHA, failure, inFlight, missing
}

var machineToken = "test-token"

// machine is one deterministic delivery environment.
type machine struct {
	t          *testing.T
	clock      *fakeClock
	agent      *fakeAgentPort
	remote     *remoteState
	ci         *ciState
	store      *workbench.Store
	manager    *Manager
	taskID     string
	deliveryID string
	tree       string
	source     string
	baseSHA    string
	roundID    string
}

// newMachine boots the whole fixture: project repo, task worktree, store with
// policy and one delivery cycle created through the real store start path.
func newMachine(t *testing.T, worktreeDirty bool) *machine {
	return newMachineWithPolicy(t, worktreeDirty, nil)
}

// newMachineWithPolicy boots like newMachine and lets a test mutate the
// delivery policy before it is first saved (cycle snapshots are frozen).
func newMachineWithPolicy(t *testing.T, worktreeDirty bool, mutate func(p *workbench.ProjectDeliveryPolicy)) *machine {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "project")
	if e := os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s", args, b)
		}
	}
	git(source, "init", "--initial-branch=main", "--template=")
	writeFile(t, filepath.Join(source, "base.txt"), "base\n")
	git(source, "add", ".")
	git(source, "commit", "-m", "base")
	base := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		b, e := c.Output()
		if e != nil {
			t.Fatalf("git %v: %s", args, b)
		}
		return strings.TrimSpace(string(b))
	}
	baseSHA := base(source, "rev-parse", "refs/heads/main")

	taskID := "11111111111111111111111111111111"
	parent := filepath.Join(root, "data", "worktrees", taskID)
	if e := os.MkdirAll(parent, 0700); e != nil {
		t.Fatal(e)
	}
	tree := filepath.Join(parent, "tree")
	git(source, "worktree", "add", "-b", "kaiwu/task-"+taskID, tree, baseSHA)
	if worktreeDirty {
		writeFile(t, filepath.Join(tree, "feature.txt"), "feature\n")
	}
	owner := fmt.Sprintf(`{"TaskID":%q,"Source":%q,"Branch":"kaiwu/task-%s","BaseSHA":%q,"Ready":true}`, taskID, source, taskID, baseSHA)
	writeFile(t, filepath.Join(parent, "owner.json"), owner)

	store, e := workbench.Open(filepath.Join(root, "data", "kaiwu.db"), workbench.Profile{Provider: "fake", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	if e = store.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", Path: source, DefaultBranch: "main", MainSeen: true, CommitName: "Fixture", CommitEmail: "fixture@example.invalid"}
		st.Tasks[taskID] = workbench.Task{ID: taskID, ProjectID: "p", ConversationID: "c", Status: "idle", Title: "交付任务", Request: "fixture"}
		if st.Conversations == nil {
			st.Conversations = map[string]workbench.Conversation{}
		}
		st.Conversations["c"] = workbench.Conversation{ID: "c", TaskID: taskID, RunState: "idle"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}

	policy, e := store.DeliveryPolicy("p")
	if e != nil {
		t.Fatal(e)
	}
	policy.Enabled = true
	policy.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	policy.Checks = []workbench.DeliveryCheck{{ID: "echo", Argv: []string{"echo", "local check ok"}, Dir: ".", TimeoutSeconds: 300}}
	policy.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	policy.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2}}
	policy.GitCredentialRef = "server_github_https"
	policy.APICredentialRef = "server_github_api"
	policy.AuthorizationDigest = "explicit-scope"
	policy.Actor = "local-login"
	policy.AuthorizedAt = time.Now().UTC()
	if mutate != nil {
		mutate(&policy)
	}
	if _, e = store.SaveDeliveryPolicy("enable-delivery", 0, policy); e != nil {
		t.Fatal(e)
	}

	r, e := store.Command(workbench.Command{OperationID: "start-delivery", Revision: store.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: taskID})
	if e != nil {
		t.Fatal(e)
	}
	if r.DeliveryID == "" {
		t.Fatal("start did not create a delivery cycle")
	}
	snap := store.Snapshot()
	x := snap.Executions[snap.Tasks[taskID].ExecutionID]
	if e = store.Update(func(st *workbench.State) error {
		x.Workspace, x.Branch, x.BaseSHA = tree, "kaiwu/task-"+taskID, baseSHA
		st.Executions[x.ID] = x
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	m := &machine{t: t, clock: newFakeClock(), agent: &fakeAgentPort{lease: &stubLease{accept: true}, claimStore: store}, remote: &remoteState{}, ci: &ciState{}, store: store, taskID: taskID, deliveryID: r.DeliveryID, tree: tree, source: source, baseSHA: baseSHA, roundID: r.CreatedID}
	cfg := Config{Agent: m.agent, Actions: m.ci.client(t, store, m.deliveryID, taskID, machineToken), Remote: gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: m.remote.transport(store, taskID)}), Clock: m.clock, DataDir: filepath.Join(root, "data")}
	ctx := context.Background()
	m.manager, e = New(ctx, store, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = m.manager.Close() })
	return m
}

// gitIn runs one git command in the fixture repository without extra config.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s", args, b)
	}
	return strings.TrimSpace(string(b))
}

// commitAllIn stages and commits the whole fixture worktree, returning HEAD.
func commitAllIn(t *testing.T, dir string) string {
	t.Helper()
	gitIn(t, dir, "add", "--all", "--", ".")
	gitIn(t, dir, "commit", "--no-gpg-sign", "-m", "recovery fixture")
	return headOf(t, dir)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}

func (m *machine) waitPhase(t *testing.T, phase string) workbench.Delivery {
	t.Helper()
	return m.waitPhaseNamed(t, m.deliveryID, phase)
}

func (m *machine) waitPhaseNamed(t *testing.T, deliveryID, phase string) workbench.Delivery {
	t.Helper()
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		// Nudge the injected clock so the periodic rescan arm fires even when
		// an event-driven wake was coalesced away; the drift is milliseconds.
		m.clock.advance(time.Millisecond)
		d, e := m.store.Delivery(deliveryID)
		if e != nil {
			t.Fatal(e)
		}
		if d.Phase == phase {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	d, e := m.store.Delivery(deliveryID)
	if e != nil {
		t.Fatal(e)
	}
	t.Fatalf("delivery stuck in %q, want %q (block=%q %s repairs=%v dispatched=%d used=%d limit=%d ci=%v)", d.Phase, phase, d.BlockCode, d.BlockReason, m.repairRoundsDebug(), m.agent.repairsCount(), d.RepairsUsed, d.RepairsLimit, m.ci.queries())
	return d
}

// repairRoundsDebug renders the repair rounds for the wait diagnostics.
func (m *machine) repairRoundsDebug() string {
	parts := []string{}
	for _, r := range m.repairRounds() {
		parts = append(parts, fmt.Sprintf("#%s:%s", r.ID[:6], r.Phase))
	}
	return strings.Join(parts, ",")
}

func (m *machine) mustDelivery() *workbench.Delivery {
	d, e := m.store.Delivery(m.deliveryID)
	if e != nil {
		m.t.Fatal(e)
	}
	return &d
}

// finishRound marks the task round finished so the machine may evaluate
// local checks; report may be nil.
func (m *machine) finishRound(report *workbench.Report) {
	m.finishRoundNamed(m.roundID, report)
}

// finishRoundNamed marks any round of the fixture conversation finished and
// settles the conversation the way a real round finish does: the session is
// idle and pending messages are accepted.
func (m *machine) finishRoundNamed(roundID string, report *workbench.Report) {
	m.finishRoundIn("c", roundID, report)
}

// finishRoundIn settles one round of any fixture conversation; the second
// task's cycle uses its own conversation.
func (m *machine) finishRoundIn(conversationID, roundID string, report *workbench.Report) {
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[conversationID]
		c.RunState = "idle"
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" {
				c.Messages[i].Delivery = "accepted"
			}
		}
		st.Conversations[conversationID] = c
		workbench.UpdateRound(st, conversationID, roundID, func(r *workbench.Round) {
			r.Phase = "finished"
			r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			if report != nil {
				r.Report = report
			}
		})
		return nil
	}); e != nil {
		m.t.Fatal(e)
	}
}

// finishRepairRound settles one automatic repair dispatch the way the agent
// port would: the round bound to the repair message finishes and the
// conversation returns to idle. An optional fix file emulates the repaired
// workspace change.
func (m *machine) finishRepairRound(messageID, fixFile string) {
	if fixFile != "" {
		writeFile(m.t, filepath.Join(m.tree, fixFile), "修复内容\n")
	}
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.RunState = "idle"
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" {
				c.Messages[i].Delivery = "accepted"
			}
		}
		st.Conversations["c"] = c
		workbench.UpdateRound(st, "c", messageID, func(r *workbench.Round) {
			r.Phase = "finished"
			r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
		return nil
	}); e != nil {
		m.t.Fatal(e)
	}
}

// addSecondTask opens a second independent task/cycle on the same machine, so
// two deliveries share one clone and one remote main lane.
func (m *machine) addSecondTask(t *testing.T) (taskID, deliveryID, roundID string) {
	t.Helper()
	taskID = strings.Repeat("2", 32)
	parent := filepath.Join(filepath.Dir(filepath.Dir(m.tree)), taskID)
	if e := os.MkdirAll(parent, 0700); e != nil {
		t.Fatal(e)
	}
	tree := filepath.Join(parent, "tree")
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", m.source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s", args, b)
		}
	}
	run("worktree", "add", "-b", "kaiwu/task-"+taskID, tree, m.baseSHA)
	writeFile(t, filepath.Join(tree, "second.txt"), "second task\n")
	owner := fmt.Sprintf(`{"TaskID":%q,"Source":%q,"Branch":"kaiwu/task-%s","BaseSHA":%q,"Ready":true}`, taskID, m.source, taskID, m.baseSHA)
	writeFile(t, filepath.Join(parent, "owner.json"), owner)
	if e := m.store.Update(func(st *workbench.State) error {
		st.Tasks[taskID] = workbench.Task{ID: taskID, ProjectID: "p", ConversationID: "c2", Status: "idle", Title: "第二交付", Request: "fixture"}
		st.Conversations["c2"] = workbench.Conversation{ID: "c2", TaskID: taskID, RunState: "idle"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	r, e := m.store.Command(workbench.Command{OperationID: "start-delivery-2", Revision: m.store.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: taskID})
	if e != nil {
		t.Fatal(e)
	}
	snap := m.store.Snapshot()
	x := snap.Executions[snap.Tasks[taskID].ExecutionID]
	if e := m.store.Update(func(st *workbench.State) error {
		x.Workspace, x.Branch, x.BaseSHA = tree, "kaiwu/task-"+taskID, m.baseSHA
		st.Executions[x.ID] = x
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return taskID, r.DeliveryID, r.CreatedID
}

// startNextCycle opens a second delivery cycle on the same machine through// the real store command path, binds the execution to the same worktree and
// returns its delivery/round IDs. The fixture marks the prior cycle's task
// state the way a settled previous round does: failed status with an idle
// conversation allows a fresh cycle through the real command path.
func (m *machine) startNextCycle(opID string) (deliveryID, roundID string) {
	if e := m.store.Update(func(st *workbench.State) error {
		t := st.Tasks[m.taskID]
		t.Status = "failed"
		st.Tasks[m.taskID] = t
		c := st.Conversations[t.ConversationID]
		c.RunState = "idle"
		st.Conversations[t.ConversationID] = c
		return nil
	}); e != nil {
		m.t.Fatal(e)
	}
	r, e := m.store.Command(workbench.Command{OperationID: opID, Revision: m.store.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: m.taskID})
	if e != nil {
		m.t.Fatal(e)
	}
	snap := m.store.Snapshot()
	x := snap.Executions[snap.Tasks[m.taskID].ExecutionID]
	if x.ID == "" {
		m.t.Fatal("second cycle created no execution")
	}
	if e := m.store.Update(func(st *workbench.State) error {
		x.Workspace, x.Branch, x.BaseSHA = m.tree, "kaiwu/task-"+m.taskID, m.baseSHA
		st.Executions[x.ID] = x
		return nil
	}); e != nil {
		m.t.Fatal(e)
	}
	return r.DeliveryID, r.CreatedID
}

func happyReport() *workbench.Report {
	return &workbench.Report{Summary: "功能已实现", Stage: "awaiting_review"}
}

// ---- Tests -----------------------------------------------------------------

func policySnapshot(t *testing.T, store *workbench.Store) workbench.ProjectDeliveryPolicy {
	t.Helper()
	p, e := store.DeliveryPolicy("p")
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestDeliveryStateMachineGates(t *testing.T) {
	t.Run("settled round advances task stages only", func(t *testing.T) {
		m := newMachine(t, true)

		// queued → developing requires a ready preflight and exactly one Send.
		d := m.waitPhase(t, "developing")
		if sends := m.agent.sent(); len(sends) != 1 || sends[0] != [2]string{"c", m.roundID} {
			t.Fatalf("wrong dispatch %v", sends)
		}
		if d.Fingerprint == "" || d.CommonDir == "" {
			t.Fatal("cycle baseline missing")
		}
		// The round is still running: no later stage may run.
		if d := m.mustDelivery(); d.Phase != "developing" {
			t.Fatalf("unsettled round advanced to %q", d.Phase)
		}

		m.finishRound(happyReport())
		d = m.waitPhase(t, "committing")
		if d.CandidateTree == "" {
			t.Fatal("candidate tree not frozen from check evidence")
		}
		// checks ran under the task only write lease
		for _, scope := range m.agent.leases() {
			if scope != "task" {
				t.Fatalf("wrong lease scope %q", scope)
			}
		}
		d = m.waitPhase(t, "task_ci")
		// the push wrote exactly the committed SHA to the task branch
		if got := m.remote.pushAt(0); got != d.CandidateSHA+" refs/heads/kaiwu/task-"+m.taskID {
			t.Fatalf("push receipt %q", got)
		}
		if m.remote.pushCount() != 1 {
			t.Fatalf("unexpected push count %d", m.remote.pushCount())
		}
		// the workspace carries the auto commit bound to the evidence tree
		revParse(t, m.tree, "rev-parse", "HEAD") // commit exists
		summary := m.store.Snapshot().Deliveries[m.taskID]
		if summary.RequireAcceptance && d.AcceptanceToken == "" {
			t.Fatal("task CI passed without an acceptance token")
		}
		d = m.waitPhase(t, "awaiting_acceptance")
		_, _, released, _ := m.agent.lease.journal()
		if !released || m.remote.pushCount() != 1 {
			t.Fatalf("lease released=%v pushes=%d", released, m.remote.pushCount())
		}
	})

	t.Run("unknown run cannot close the CI gate", func(t *testing.T) {
		m := newMachine(t, true)
		m.ci.setOpen(true, false, false, false)
		m.finishRound(happyReport())
		m.waitPhase(t, "task_ci")
		if m.remote.pushCount() != 1 {
			t.Fatalf("wrong run triggered extra pushes: %d", m.remote.pushCount())
		}
		summary := m.store.Snapshot().Deliveries[m.taskID]
		if len(summary.CI) != 0 {
			t.Fatalf("wrong-run observation persisted: %+v", summary.CI)
		}
		m.ci.setOpen(true, false, false, true)
		m.clock.advance(time.Minute)
		if d := m.mustDelivery(); d.Phase != "task_ci" {
			t.Fatalf("missing runs advanced to %q", d.Phase)
		}
		// now the exact run completes the gate
		m.ci.setOpen(false, false, false, false)
		m.clock.advance(time.Second)
		m.waitPhase(t, "awaiting_acceptance")
	})

	t.Run("failed CI blocks with recorded evidence", func(t *testing.T) {
		m := newMachine(t, true)
		m.ci.setOpen(false, true, false, false)
		m.finishRound(happyReport())
		d := m.waitPhase(t, "blocked")
		if d.BlockCode == "" {
			t.Fatal("failed CI blocked without a code")
		}
		summary := m.store.Snapshot().Deliveries[m.taskID]
		if len(summary.CI) != 1 || summary.CI[0].Conclusion != "failure" {
			t.Fatalf("failure evidence missing: %+v", summary.CI)
		}
		if m.remote.pushCount() != 1 {
			t.Fatalf("pushes after failed CI: %d", m.remote.pushCount())
		}
	})

	t.Run("CI deadline comes from persisted deadline, not real sleeps", func(t *testing.T) {
		m := newMachine(t, true)
		m.ci.setOpen(false, false, true, false) // run never completes
		m.finishRound(happyReport())
		m.waitPhase(t, "task_ci")
		m.clock.advance(3 * time.Hour)
		d := m.waitPhase(t, "blocked")
		if d.BlockCode == "" {
			t.Fatal("CI deadline did not trip")
		}
	})

	t.Run("no valid change does not duplicate a commit", func(t *testing.T) {
		m := newMachine(t, false)
		m.finishRound(happyReport())
		m.waitPhase(t, "pushing_task")
		d := m.mustDelivery()
		if d.CandidateSHA != m.baseSHA {
			t.Fatalf("commit created without valid change: %s", d.CandidateSHA)
		}
		m.waitPhase(t, "awaiting_acceptance")
		if m.remote.pushCount() != 1 || m.remote.pushAt(0) != m.baseSHA+" refs/heads/kaiwu/task-"+m.taskID {
			t.Fatalf("base SHA not pushed as candidate: %q", m.remote.pushAt(0))
		}
	})

	t.Run("revocation stops external writes immediately", func(t *testing.T) {
		m := newMachine(t, true)
		d := m.waitPhase(t, "developing")
		p := policySnapshot(t, m.store)
		p.Enabled = false
		if _, e := m.store.SaveDeliveryPolicy("disable-cycle", p.Version, p); e != nil {
			t.Fatal(e)
		}
		m.finishRound(happyReport())
		blocked := m.waitPhase(t, "blocked")
		if blocked.BlockCode != "authorization_disabled" {
			t.Fatalf("wrong block code %q", blocked.BlockCode)
		}
		if blocked.Version <= d.Version {
			t.Fatalf("version history wrong: %d", blocked.Version)
		}
		if pushed := m.remote.pushCount(); pushed != 0 {
			t.Fatalf("push after revocation: %d", pushed)
		}
	})
}

func TestDeliveryNoImplicitStartAndPreflightReads(t *testing.T) {
	t.Run("disabled delivery never opens a cycle", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "project")
		if e := os.MkdirAll(source, 0700); e != nil {
			t.Fatal(e)
		}
		if c := exec.Command("git", "-C", source, "init", "--initial-branch=main", "--template="); c.Run() != nil {
			t.Fatal("git init")
		}
		store, e := workbench.Open(filepath.Join(root, "kaiwu.db"), workbench.Profile{Provider: "fake", Model: "fake"})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = store.Close() })
		if e = store.Update(func(st *workbench.State) error {
			st.Projects["p"] = workbench.Project{ID: "p", Path: source, DefaultBranch: "main", MainSeen: true}
			st.Tasks["22222222222222222222222222222222"] = workbench.Task{ID: "22222222222222222222222222222222", ProjectID: "p", ConversationID: "c", Status: "idle", Title: "t", Request: "r"}
			st.Conversations["c"] = workbench.Conversation{ID: "c", TaskID: "22222222222222222222222222222222", RunState: "idle"}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		r, e := store.Command(workbench.Command{OperationID: "start-disabled", Revision: store.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: "22222222222222222222222222222222"})
		if e != nil {
			t.Fatal(e)
		}

		if r.DeliveryID != "" {
			t.Fatalf("disabled delivery opened a cycle %q", r.DeliveryID)
		}
		active, e := store.ActiveDeliveries()
		if e != nil {
			t.Fatal(e)
		}
		if len(active) != 0 {
			t.Fatalf("cycles exist: %+v", active)
		}
	})

	t.Run("preflight is read-only and reports blockers", func(t *testing.T) {
		m := newMachine(t, true)
		m.finishRound(happyReport())
		// Fixture correction (GREEN): a healthy fresh fixture cannot block —
		// the approved pipeline drives queued→…→awaiting_acceptance and every
		// gate passes here. awaiting_acceptance is the phase where the
		// autonomous loop stops writing, which is what this test needs.
		m.waitPhase(t, "awaiting_acceptance")
		p := policySnapshot(t, m.store)
		project := m.store.Snapshot().Projects["p"]
		memBefore := m.remote.pushCount()
		pf, e := m.manager.Preflight(context.Background(), project, p)
		if e != nil {
			t.Fatal(e)
		}
		if !pf.Ready || pf.Fingerprint != workbench.ProtectedDeliveryFingerprint(p) {
			t.Fatalf("preflight rejected a healthy project: %+v", pf)
		}
		if m.remote.pushCount() != memBefore {
			t.Fatalf("preflight wrote remotely: %d", m.remote.pushCount())
		}
		// repo identity mismatch must surface as a blocker
		p.Repository.ID = 43
		pf, e = m.manager.Preflight(context.Background(), project, p)
		if e != nil {
			t.Fatal(e)
		}
		if pf.Ready || len(pf.Blockers) == 0 {
			t.Fatalf("mismatch not reported: %+v", pf)
		}
	})
}

func TestDeliveryWakeIdempotent(t *testing.T) {
	m := newMachine(t, true)
	m.waitPhase(t, "developing")
	// Additional wakes must not dispatch the same round again.
	for i := 0; i < 5; i++ {
		m.manager.Wake()
	}
	time.Sleep(50 * time.Millisecond)
	m.manager.Wake()
	if sends := m.agent.sent(); len(sends) != 1 {
		t.Fatalf("wake fan-out duplicated dispatch: %d sends", len(sends))
	}
}
