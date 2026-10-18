package webapi

import (
	"net/http"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Interaction answers are recorded first and dispatched afterwards. A failing
// transport is reported as a durable delivery state the client may retry with
// retry_interaction_answer; the HTTP receipt itself must not claim delivery.

func (h *handler) interactionEnvelope(receipt workbench.Receipt) workbench.InteractionReceipt {
	req, err := h.store.Interaction(receipt.ConversationID, receipt.InteractionID)
	if err == nil && req != nil && req.Response != nil {
		receipt.Delivery = req.Response.Delivery
	}
	return workbench.InteractionReceipt{Receipt: receipt, Interaction: req}
}

func (h *handler) dispatchInteraction(receipt workbench.Receipt) string {
	req, err := h.store.Interaction(receipt.ConversationID, receipt.InteractionID)
	if err != nil || req.Response == nil || req.Response.Delivery != workbench.DeliveryRecorded {
		return ""
	}
	if req.Source == workbench.SourceAgentUI {
		if h.agent == nil {
			h.setInteractionDelivery(receipt, workbench.DeliveryNotSent, "服务未装配 pi 会话；答复已记录，可稍后重试发送")
			return "答复已记录；pi 会话未启动，可稍后重试发送"
		}
		if err := h.agent.AnswerInteraction(receipt.ConversationID, receipt.InteractionID); err != nil {
			return err.Error()
		}
		return ""
	}
	messageID := req.Response.MessageID
	if messageID == "" {
		// The answer is recorded but other blocking questions are still open;
		// the aggregated turn is sent once when the last one is answered.
		return ""
	}
	if h.agent == nil {
		h.interactionSendFailed(receipt, messageID, "服务未找到 pi；答复已记录，可稍后再试")
		return "答复已记录；pi 会话未启动，可稍后重试发送"
	}
	if err := h.agent.Send(receipt.ConversationID, messageID); err != nil {
		h.interactionSendFailed(receipt, messageID, "原会话未启动；答复已记录，未自动重试")
		return "答复已记录；原会话未启动，未自动重试"
	}
	return ""
}

func (h *handler) setInteractionDelivery(receipt workbench.Receipt, delivery, note string) {
	_ = h.store.Update(func(st *workbench.State) error {
		workbench.SetInteractionDelivery(st, receipt.ConversationID, receipt.InteractionID, delivery, note)
		return nil
	})
}

// interactionSendFailed keeps the recorded answer and the message, but marks
// the conversation as not started so the client shows "答复已记录，送达待重试".
func (h *handler) interactionSendFailed(receipt workbench.Receipt, messageID, reason string) {
	_ = h.store.Update(func(st *workbench.State) error {
		c := st.Conversations[receipt.ConversationID]
		if c.RunState != "running" {
			c.RunState = "failed"
		}
		c.Error = reason
		for i := range c.Messages {
			if c.Messages[i].ID == messageID && c.Messages[i].Delivery == "pending" {
				c.Messages[i].Delivery = "not_sent"
			}
		}
		st.Conversations[c.ID] = c
		workbench.SyncConversationInteractionsFromMessages(st, c.ID)
		workbench.SetInteractionDelivery(st, receipt.ConversationID, receipt.InteractionID, workbench.DeliveryNotSent, reason)
		if t, ok := st.Tasks[c.TaskID]; ok && t.ExecutionID != "" {
			x := st.Executions[t.ExecutionID]
			if x.Phase == "queued" || x.Phase == "starting" {
				x.Phase = "failed"
				x.Error = reason
				st.Executions[x.ID] = x
				t.Status = "failed"
				st.Tasks[t.ID] = t
			}
		}
		return nil
	})
}

// interactionAnswerGuard runs before the generic command checks: recording an
// answer never requires a running model, and a missing pi must not discard it.
func interactionAnswerGuard(w http.ResponseWriter, c workbench.Command) bool {
	if !workbench.IsInteractionCommand(c.Type) {
		return true
	}
	if c.ProjectID == "" || c.ConversationID == "" || c.RequestID == "" || c.RequestVersion < 1 {
		fail(w, 400, "invalid_input", "交互答复缺少请求身份或版本")
		return false
	}
	return true
}
