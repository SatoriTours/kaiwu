package githubactions

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// RunKey identifies the exact gate: repository, stage, branch, SHA and the
// required workflows. Event is fixed to push.
type RunKey struct {
	RepositoryID  int64
	DeliveryID    string
	Stage         string
	Owner         string
	Repo          string
	Branch        string
	SHA           string
	CredentialRef string
	Required      []workbench.DeliveryWorkflow
}

// Observation aggregates the gate result for one Observe call.
type Observation struct {
	Runs    []workbench.DeliveryCI
	State   string
	Code    string
	RetryAt time.Time
}

type runRow struct {
	ID         int64     `json:"id"`
	RunAttempt int       `json:"run_attempt"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion *string   `json:"conclusion"`
	HeadBranch string    `json:"head_branch"`
	HeadSHA    string    `json:"head_sha"`
	WorkflowID int64     `json:"workflow_id"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url"`
}

type runsBody struct {
	TotalCount int      `json:"total_count"`
	Runs       []runRow `json:"workflow_runs"`
}

func (r runRow) conclusion() string {
	if r.Conclusion == nil {
		return ""
	}
	return *r.Conclusion
}

// newer reports whether row a should replace b as the current run.
func newer(a, b runRow) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.ID > b.ID
	}
	return a.CreatedAt.After(b.CreatedAt)
}

// runsForWorkflow lists runs of one workflow with SHA and branch filters,
// verifying every row client-side. complete is false when pagination was
// exhausted without proving the full result set; reused reports whether the
// last page was served from a 304 cache hit.
func (c *Client) runsForWorkflow(ctx context.Context, key RunKey, workflowID int64) (rows []runRow, complete bool, reused bool, err error) {
	const pathFmt = "/repos/%s/%s/actions/workflows/%d/runs"
	base := fmt.Sprintf(pathFmt, key.Owner, key.Repo, workflowID)
	for p := 1; p <= maxPages; p++ {
		query := url.Values{
			"head_sha": {key.SHA},
			"branch":   {key.Branch},
			"event":    {"push"},
			"per_page": {fmt.Sprint(perPage)},
			"page":     {fmt.Sprint(p)},
		}
		resp, e := c.fetch(ctx, key.CredentialRef, base, query, true)
		if e != nil {
			return nil, false, false, e
		}
		if resp.notModified {
			reused = true
		}
		var body runsBody
		if e := decode(resp.content, &body); e != nil {
			return nil, false, false, e
		}
		for _, row := range body.Runs {
			// Never trust server-side filters alone.
			if row.Event != "push" || row.HeadSHA != key.SHA || row.HeadBranch != key.Branch {
				continue
			}
			rows = append(rows, row)
		}
		next := parsePage(resp.header).next
		if next == 0 {
			return rows, true, reused, nil
		}
		if p == maxPages {
			return rows, false, reused, nil
		}
	}
	return rows, false, reused, nil
}

// Observe evaluates the exact CI gate for the current candidate.
func (c *Client) Observe(ctx context.Context, key RunKey) (Observation, error) {
	obs := Observation{State: StateWaiting}
	if len(key.Required) == 0 {
		return obs, &APIError{State: StateBlocked, Code: "no_required_workflows", err: fmt.Errorf("必需 workflow 为空")}
	}
	now := time.Now().UTC()
	waitAt := now.Add(retryDelay)
	result := Observation{State: StatePassed, RetryAt: waitAt}
	blocked := ""
	failed := ""
	waiting := ""

	for _, wf := range key.Required {
		rows, complete, reused, err := c.runsForWorkflow(ctx, key, wf.ID)
		if err != nil {
			var api *APIError
			if asAPI(err, &api) {
				return Observation{State: api.State, Code: api.Code, RetryAt: api.RetryAt}, nil
			}
			return Observation{State: StateBlocked, Code: "protocol_error"}, err
		}
		if !complete {
			return Observation{State: StateBlocked, Code: "pagination_incomplete"}, nil
		}
		if reused {
			// A 304 only proves nothing changed: never a new success.
			return Observation{State: StateWaiting, Code: "not_modified", RetryAt: waitAt}, nil
		}
		if len(rows) == 0 {
			waiting = "no_matching_run"
			continue
		}
		current := rows[0]
		for _, row := range rows[1:] {
			if newer(row, current) {
				current = row
			}
		}
		ci := workbench.DeliveryCI{
			DeliveryID: key.DeliveryID, Stage: key.Stage,
			Branch: key.Branch, SHA: key.SHA, Event: "push",
			Status: current.Status, Conclusion: current.conclusion(),
			URL: current.HTMLURL, RepositoryID: key.RepositoryID,
			WorkflowID: wf.ID, RunID: current.ID, Attempt: current.RunAttempt,
			Jobs: map[string]string{}, ObservedAt: now,
		}
		switch current.Status {
		case "completed":
		case "", "queued", "in_progress", "waiting", "requested", "pending":
			waiting = "not_completed"
			result.Runs = append(result.Runs, ci)
			continue
		default:
			waiting = "not_completed"
			result.Runs = append(result.Runs, ci)
			continue
		}
		state, code, repairable := mapConclusion(current.conclusion())
		if hasRequiredJobs(wf) {
			jobs, complete, e := c.jobsForAttempt(ctx, key, current.ID, current.RunAttempt)
			if e != nil {
				var api *APIError
				if asAPI(e, &api) {
					return Observation{State: api.State, Code: api.Code, RetryAt: api.RetryAt}, nil
				}
				return Observation{State: StateBlocked, Code: "protocol_error"}, e
			}
			ci.Jobs = jobMap(jobs)
			if state == StatePassed {
				if !complete {
					return Observation{State: StateBlocked, Code: "pagination_incomplete"}, nil
				}
				state, code, repairable = gateJobs(wf, jobs)
			}
		}
		result.Runs = append(result.Runs, ci)
		switch state {
		case StateBlocked:
			if blocked == "" {
				blocked = code
			}
		case StateFailed:
			if failed == "" {
				failed = code
				if !repairable {
					failed += ":not_repairable"
				}
			}
		}
	}
	result.Code = first(blocked, failed, waiting, "")
	switch {
	case blocked != "":
		result.State = StateBlocked
	case failed != "":
		result.State = StateFailed
	case waiting != "":
		result.State = StateWaiting
	default:
		result.Code = ""
	}
	return result, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func asAPI(err error, target **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*target = e
		return true
	}
	return false
}
