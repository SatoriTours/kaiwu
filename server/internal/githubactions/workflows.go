package githubactions

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// WorkflowRef is a repository workflow definition as seen read-only.
type WorkflowRef struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
	Path  string `json:"path"`
	Name  string `json:"name"`
}

// RepositoryInfo is the read-only preflight projection of a repository.
type RepositoryInfo struct {
	ID            int64         `json:"id"`
	Owner         string        `json:"owner"`
	Name          string        `json:"name"`
	DefaultBranch string        `json:"defaultBranch"`
	Workflows     []WorkflowRef `json:"workflows"`
	Blockers      []string      `json:"blockers"`
}

type repoBody struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

type workflowsBody struct {
	TotalCount int           `json:"total_count"`
	Workflows  []WorkflowRef `json:"workflows"`
}

// Inspect verifies the repository identity against the authorized repository
// ID and reports workflow definitions plus read-only preflight blockers.
func (c *Client) Inspect(ctx context.Context, repositoryID int64, owner, repo, credentialRef string) (RepositoryInfo, error) {
	info := RepositoryInfo{Owner: owner, Name: repo}
	var body repoBody
	resp, err := c.fetch(ctx, credentialRef, "/repos/"+owner+"/"+repo, nil, false)
	if err != nil {
		return info, err
	}
	if e := decode(resp.content, &body); e != nil {
		return info, e
	}
	if body.ID != repositoryID {
		return info, &APIError{State: StateBlocked, Code: "repository_mismatch", Status: resp.status,
			err: fmt.Errorf("仓库 ID %d 与授权 %d 不一致", body.ID, repositoryID)}
	}
	info.ID = body.ID
	info.DefaultBranch = body.DefaultBranch

	const workflowsPath = "/repos/%s/%s/actions/workflows"
	for p := 1; p <= maxPages; p++ {
		query := url.Values{"per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(p)}}
		resp, e := c.fetch(ctx, credentialRef, fmt.Sprintf(workflowsPath, owner, repo), query, false)
		if e != nil {
			return info, e
		}
		var page workflowsBody
		if e := decode(resp.content, &page); e != nil {
			return info, e
		}
		info.Workflows = append(info.Workflows, page.Workflows...)
		if parsePage(resp.header).next == 0 {
			break
		}
		if p == maxPages {
			info.Blockers = append(info.Blockers, "workflow 列表分页未遍历完整")
		}
	}
	for _, wf := range info.Workflows {
		if !strings.EqualFold(wf.State, "active") {
			info.Blockers = append(info.Blockers, fmt.Sprintf("workflow %d(%s) 状态为 %s，不会触发", wf.ID, wf.Path, wf.State))
		}
	}
	return info, nil
}
