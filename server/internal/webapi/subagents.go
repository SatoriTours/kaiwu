package webapi

import (
	"net/http"
)

// The subagent endpoints are read-only on purpose. Runs are recorded by the
// controlled server-side runtime through the workbench Store; there is no
// HTTP or socket method that creates, updates or controls a run.
type subagentDetailRequest struct {
	ProjectID      string `json:"projectId"`
	ConversationID string `json:"conversationId"`
	RunID          string `json:"runId"`
	After          uint64 `json:"after"`
	Limit          int    `json:"limit"`
}

func (h *handler) subagentDetail(w http.ResponseWriter, r *http.Request) {
	var input subagentDetailRequest
	if !decode(w, r, &input) {
		return
	}
	page, err := h.store.SubagentDetail(input.ProjectID, input.ConversationID, input.RunID, input.After, input.Limit)
	if err != nil {
		h.failure(w, err)
		return
	}
	respond(w, 200, page)
}
