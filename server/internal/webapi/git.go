package webapi

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"net/http"
	"strings"
)

type GitCommander interface {
	PreviewMerge(context.Context, string, string) (workbench.MergePreview, error)
	StartMerge(workbench.MergeRequest) (workbench.MergeOperation, bool, error)
	RefreshGit(context.Context, string) error
	UpdateProjectGit(context.Context, workbench.Command) (workbench.Receipt, error)
}

func (h *handler) git(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Git == nil {
		fail(w, 503, "git_unavailable", "Git 集成不可用")
		return
	}
	switch {
	case r.URL.Path == "/api/git/refresh" && r.Method == "POST":
		var input struct {
			ProjectID string `json:"projectId"`
		}
		if !decode(w, r, &input) {
			return
		}
		if e := h.cfg.Git.RefreshGit(r.Context(), input.ProjectID); e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/git/preview" && r.Method == "POST":
		var input struct {
			ProjectID string `json:"projectId"`
			TaskID    string `json:"taskId"`
		}
		if !decode(w, r, &input) {
			return
		}
		p, e := h.cfg.Git.PreviewMerge(r.Context(), input.ProjectID, input.TaskID)
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, 200, p)
	case r.URL.Path == "/api/git/merge" && r.Method == "POST":
		if !pirpc.ProcessGroupsSupported() {
			fail(w, 503, "merge_unavailable", "当前平台不支持 Git 写入集成")
			return
		}
		var input workbench.MergeRequest
		if !decode(w, r, &input) {
			return
		}
		op, _, e := h.cfg.Git.StartMerge(input)
		if e != nil {
			h.failure(w, e)
			return
		}
		respond(w, http.StatusAccepted, op)
	case strings.HasPrefix(r.URL.Path, "/api/git/operations/") && r.Method == "GET":
		id := strings.TrimPrefix(r.URL.Path, "/api/git/operations/")
		op, ok := h.store.Snapshot().Merges[id]
		if !ok {
			h.failure(w, workbench.Missing())
			return
		}
		respond(w, 200, op)
	default:
		fail(w, 404, "not_found", "Git API 不存在")
	}
}
