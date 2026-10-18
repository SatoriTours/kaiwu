package githubactions

import (
	"context"
	"fmt"
	"net/url"
)

type jobRow struct {
	ID         int64   `json:"id"`
	RunID      int64   `json:"run_id"`
	RunAttempt int     `json:"run_attempt"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	HeadSHA    string  `json:"head_sha"`
	HeadBranch string  `json:"head_branch"`
	HTMLURL    string  `json:"html_url"`
}

func (j jobRow) conclusion() string {
	if j.Conclusion == nil {
		return ""
	}
	return *j.Conclusion
}

type jobsBody struct {
	TotalCount int      `json:"total_count"`
	Jobs       []jobRow `json:"jobs"`
}

// jobsForAttempt lists all jobs of one run attempt with bounded pagination.
func (c *Client) jobsForAttempt(ctx context.Context, key RunKey, runID int64, attempt int) ([]jobRow, bool, error) {
	const pathFmt = "/repos/%s/%s/actions/runs/%d/attempts/%d/jobs"
	base := fmt.Sprintf(pathFmt, key.Owner, key.Repo, runID, attempt)
	var all []jobRow
	for p := 1; p <= maxPages; p++ {
		query := url.Values{"per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(p)}}
		resp, err := c.fetch(ctx, key.CredentialRef, base, query, true)
		if err != nil {
			return nil, false, err
		}
		var body jobsBody
		if e := decode(resp.content, &body); e != nil {
			return nil, false, e
		}
		for _, job := range body.Jobs {
			// Only jobs of the tracked attempt on the tracked SHA count.
			if job.RunAttempt != attempt || (job.HeadSHA != "" && job.HeadSHA != key.SHA) {
				continue
			}
			all = append(all, job)
		}
		if parsePage(resp.header).next == 0 {
			return all, true, nil
		}
		if p == maxPages {
			return all, false, nil
		}
	}
	return all, false, nil
}
