package githubactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const truncationMarker = "\n[...日志已截断...]\n"

// FailureEvidence is bounded, sanitized failure context for a repair round.
// It deliberately has no URL field: temporary download links never reach
// persisted records.
type FailureEvidence struct {
	Code        string
	Summary     string
	Fingerprint string
	Truncated   bool
	Repairable  bool
}

var ghTokenPattern = regexp.MustCompile(`gh[posr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255}`)

// Failure collects bounded sanitized evidence for a failed observation. The
// observation must be failed with a recorded run, attempt and conclusion.
func (c *Client) Failure(ctx context.Context, key RunKey, observation Observation) (FailureEvidence, error) {
	ev := FailureEvidence{}
	run, ok := failedRun(observation)
	if !ok {
		return ev, &APIError{State: StateBlocked, Code: "no_failed_run", err: fmt.Errorf("没有可取证的失败运行")}
	}
	jobs, complete, err := c.jobsForAttempt(ctx, key, run.RunID, run.Attempt)
	if err != nil {
		return ev, err
	}
	if !complete {
		return ev, &APIError{State: StateBlocked, Code: "pagination_incomplete"}
	}
	jobID := firstFailedJob(jobs)
	if jobID == 0 {
		return ev, &APIError{State: StateBlocked, Code: "no_failed_job", err: fmt.Errorf("失败运行中没有失败 job")}
	}
	log, truncated, err := c.jobLogs(ctx, key, jobLogPath(key, jobID))
	if err != nil {
		return ev, err
	}
	sanitized := c.sanitize(ctx, key.CredentialRef, log)
	ev.Summary = summarize(sanitized, SummaryLimit)
	ev.Truncated = truncated
	ev.Code = observation.Code
	// Genuine source-code failures are repairable; every other conclusion
	// (cancelled, timed_out, skipped, ...) is not.
	ev.Repairable = observation.Code == "failure"
	sum := sha256.Sum256([]byte(ev.Summary))
	ev.Fingerprint = hex.EncodeToString(sum[:])
	return ev, nil
}

// failedRun picks the failed run record from the observation. Runs were
// already resolved to the newest run per workflow by Observe.
func failedRun(observation Observation) (workbench.DeliveryCI, bool) {
	for _, ci := range observation.Runs {
		if ci.RunID == 0 || ci.Attempt == 0 || ci.Status != "completed" {
			continue
		}
		if state, _, _ := mapConclusion(ci.Conclusion); state == StateFailed {
			return ci, true
		}
	}
	return workbench.DeliveryCI{}, false
}

func firstFailedJob(jobs []jobRow) int64 {
	for _, job := range jobs {
		switch job.conclusion() {
		case "failure", "timed_out", "cancelled", "action_required", "stale":
			return job.ID
		}
	}
	return 0
}

func jobLogPath(key RunKey, jobID int64) string {
	return fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", key.Owner, key.Repo, jobID)
}

// jobLogs follows the job log redirect and reads at most LogReadLimit bytes.
// Authorization is dropped by the client's redirect policy before the
// temporary log host is contacted; the temporary URL itself is never kept.
func (c *Client) jobLogs(ctx context.Context, key RunKey, path string) (string, bool, error) {
	resp, err := c.fetchBounded(ctx, key.CredentialRef, path, nil, LogReadLimit, false)
	if err != nil {
		return "", false, err
	}
	return string(resp.content), resp.truncated, nil
}

// sanitize removes the resolved token and common GitHub token formats. Logs
// are untrusted data: they are only ever stored as inert text, never parsed
// into commands.
func (c *Client) sanitize(ctx context.Context, credentialRef, log string) string {
	out := ghTokenPattern.ReplaceAllString(log, "[已脱敏令牌]")
	if c.token != nil {
		if token, e := c.token(ctx, credentialRef); e == nil && token != "" {
			out = strings.ReplaceAll(out, token, "[已脱敏令牌]")
		}
	}
	return out
}

// summarize keeps head and tail of the sanitized log within the limit.
func summarize(log string, limit int) string {
	if len(log) <= limit {
		return log
	}
	marker := truncationMarker
	head := limit/2 - len(marker)/2
	tail := limit - len(marker) - head
	if head < 0 || tail < 0 {
		return log[:limit]
	}
	return log[:head] + marker + log[len(log)-tail:]
}
