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

// gitPreviewInput adds the optional review mode to the unchanged legacy
// preview request. Unknown modes are refused instead of silently downgrading.
type gitPreviewInput struct {
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
	Mode      string `json:"mode,omitempty"`
	RoundID   string `json:"roundId,omitempty"`
}

// reviewedGitCommander is the optional extension used for reviewed previews.
// Keeping it separate leaves existing fake Git commanders unchanged.
type reviewedGitCommander interface {
	PreviewMergeReview(context.Context, workbench.MergeReviewRequest) (workbench.MergePreview, error)
	PreviewPushReview(context.Context, workbench.MergeReviewRequest) (workbench.MergeReview, error)
}

func (h *handler) previewMerge(r *http.Request, input gitPreviewInput) (any, error) {
	switch input.Mode {
	case "":
		return h.cfg.Git.PreviewMerge(r.Context(), input.ProjectID, input.TaskID)
	case workbench.MergeModeLocal:
		reviewed, ok := h.cfg.Git.(reviewedGitCommander)
		if !ok {
			return nil, workbench.Conflict("当前 Git 实现不支持审核式合并预览")
		}
		return reviewed.PreviewMergeReview(r.Context(), workbench.MergeReviewRequest{ProjectID: input.ProjectID, TaskID: input.TaskID, Mode: input.Mode, RoundID: input.RoundID})
	case workbench.MergeModePush:
		reviewed, ok := h.cfg.Git.(reviewedGitCommander)
		if !ok {
			return nil, workbench.Conflict("当前 Git 实现不支持远程审核")
		}
		return reviewed.PreviewPushReview(r.Context(), workbench.MergeReviewRequest{ProjectID: input.ProjectID, TaskID: input.TaskID, Mode: input.Mode, RoundID: input.RoundID})
	default:
		return nil, workbench.Conflict("不支持的审核模式")
	}
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
		var input gitPreviewInput
		if !decode(w, r, &input) {
			return
		}
		p, e := h.previewMerge(r, gitPreviewInput{ProjectID: input.ProjectID, TaskID: input.TaskID, Mode: input.Mode, RoundID: input.RoundID})
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
