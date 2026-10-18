package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/delivery"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// DeliveryCommander is the narrow delivery surface the Web API may use. It has
// no free-form merge, no argv execution and no credential-name parameter:
// preflight is read-only, accept and control are the two explicit user
// operations, and Wake only asks the scheduler to look again.
type DeliveryCommander interface {
	Preflight(ctx context.Context, p workbench.Project, policy workbench.ProjectDeliveryPolicy) (delivery.Preflight, error)
	Accept(ctx context.Context, operationID, deliveryID, token string, expectedVersion uint64) (workbench.Receipt, error)
	Control(ctx context.Context, operationID, deliveryID, action string, expectedVersion uint64) (workbench.Receipt, error)
	Wake()
}

type deliveryPolicyRequest struct {
	OperationID     string                          `json:"operationId"`
	ExpectedVersion uint64                          `json:"expectedVersion"`
	Policy          workbench.ProjectDeliveryPolicy `json:"policy"`
}

type deliveryIdentityRequest struct {
	ProjectID  string `json:"projectId,omitempty"`
	TaskID     string `json:"taskId,omitempty"`
	DeliveryID string `json:"deliveryId,omitempty"`
}

type deliveryAcceptRequest struct {
	OperationID     string `json:"operationId"`
	DeliveryID      string `json:"deliveryId"`
	Token           string `json:"token"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

type deliveryControlRequest struct {
	OperationID     string `json:"operationId"`
	DeliveryID      string `json:"deliveryId"`
	Action          string `json:"action"`
	ExpectedVersion uint64 `json:"expectedVersion"`
}

type deliveryStepsRequest struct {
	DeliveryID string `json:"deliveryId"`
	After      uint64 `json:"after"`
	Limit      int    `json:"limit"`
}

// delivery routes the fixed delivery surface. Every request carries the
// project/task/delivery identity plus operationId and expectedVersion, so a
// retried operation returns its original receipt instead of repeating a side
// effect.
func (h *handler) delivery(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/api/delivery/") {
	case "policy":
		h.deliveryPolicy(w, r)
	case "preflight":
		h.deliveryPreflight(w, r)
	case "accept":
		h.deliveryAccept(w, r)
	case "control":
		h.deliveryControl(w, r)
	case "status":
		h.deliveryStatus(w, r)
	case "steps":
		h.deliverySteps(w, r)
	default:
		fail(w, 404, "not_found", "交付接口不存在")
	}
}

func (h *handler) deliveryPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		project := r.URL.Query().Get("projectId")
		if project == "" {
			fail(w, 400, "invalid_input", "缺少项目标识")
			return
		}
		p, err := h.store.DeliveryPolicy(project)
		if err != nil {
			h.failure(w, err)
			return
		}
		respond(w, 200, map[string]any{"policy": p})
		return
	}
	var in deliveryPolicyRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Policy.ProjectID == "" {
		fail(w, 400, "invalid_input", "缺少项目标识")
		return
	}
	receipt, err := h.store.SaveDeliveryPolicy(in.OperationID, in.ExpectedVersion, in.Policy)
	if err != nil {
		h.failure(w, err)
		return
	}
	respond(w, 200, receipt)
}

// deliveryPreflight is read-only: it reports readiness and blockers without
// touching Git or Actions.
func (h *handler) deliveryPreflight(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Delivery == nil {
		fail(w, 503, "delivery_unavailable", "自动交付未装配")
		return
	}
	var in deliveryIdentityRequest
	if !decode(w, r, &in) {
		return
	}
	project, ok := h.store.Snapshot().Projects[in.ProjectID]
	if !ok {
		fail(w, 404, "not_found", "项目不存在")
		return
	}
	policy, err := h.store.DeliveryPolicy(project.ID)
	if err != nil {
		h.failure(w, err)
		return
	}
	report, err := h.cfg.Delivery.Preflight(r.Context(), project, policy)
	if err != nil {
		h.failure(w, err)
		return
	}
	respond(w, 200, report)
}

func (h *handler) deliveryAccept(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Delivery == nil {
		fail(w, 503, "delivery_unavailable", "自动交付未装配")
		return
	}
	var in deliveryAcceptRequest
	if !decode(w, r, &in) {
		return
	}
	receipt, err := h.cfg.Delivery.Accept(r.Context(), in.OperationID, in.DeliveryID, in.Token, in.ExpectedVersion)
	if err != nil {
		h.failure(w, err)
		return
	}
	if !receipt.Replay {
		h.cfg.Delivery.Wake()
	}
	respond(w, 200, receipt)
}

func (h *handler) deliveryControl(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Delivery == nil {
		fail(w, 503, "delivery_unavailable", "自动交付未装配")
		return
	}
	var in deliveryControlRequest
	if !decode(w, r, &in) {
		return
	}
	receipt, err := h.cfg.Delivery.Control(r.Context(), in.OperationID, in.DeliveryID, in.Action, in.ExpectedVersion)
	if err != nil {
		h.failure(w, err)
		return
	}
	if !receipt.Replay {
		h.cfg.Delivery.Wake()
	}
	respond(w, 200, receipt)
}

// deliveryStatus projects the safe cycle summary of one task: no workspace
// path, no credential and no frozen private state.
func (h *handler) deliveryStatus(w http.ResponseWriter, r *http.Request) {
	var in deliveryIdentityRequest
	if !decode(w, r, &in) {
		return
	}
	if in.TaskID == "" {
		fail(w, 400, "invalid_input", "缺少任务标识")
		return
	}
	summary, ok := h.store.Snapshot().Deliveries[in.TaskID]
	if !ok || summary.ID == "" {
		respond(w, 200, map[string]any{"delivery": nil})
		return
	}
	respond(w, 200, map[string]any{"delivery": summary})
}

func (h *handler) deliverySteps(w http.ResponseWriter, r *http.Request) {
	var in deliveryStepsRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Limit < 1 || in.Limit > 100 {
		fail(w, 400, "invalid_input", "步骤分页大小需在 1–100")
		return
	}
	page, err := h.store.DeliverySteps(in.DeliveryID, in.After, in.Limit)
	if err != nil {
		h.failure(w, err)
		return
	}
	respond(w, 200, page)
}
