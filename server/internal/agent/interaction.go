package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const interactionTag = "<kaiwu-interaction>"

// settleRoundInteraction runs at the only reliable round boundary
// (acknowledged + settled). It publishes at most one structured request and
// reports whether the round waits for the user instead of being a result.
func settleRoundInteraction(st *workbench.State, conversationID, roundID, sourceMessageID string) bool {
	c, ok := st.Conversations[conversationID]
	if !ok || sourceMessageID == "" {
		return false
	}
	text := ""
	for i := range c.Messages {
		if c.Messages[i].ID == sourceMessageID && c.Messages[i].Role == "assistant" {
			text = c.Messages[i].Text
			break
		}
	}
	if text == "" {
		return false
	}
	report, _ := workbench.ParseReport(text)
	if report == nil {
		for i := range c.Rounds {
			if c.Rounds[i].ID == roundID {
				report = c.Rounds[i].Report
				break
			}
		}
	}
	block, visible := workbench.ParseInteractionBlock(text)
	if block == nil {
		if strings.Contains(text, interactionTag) {
			markInteractionError(st, conversationID, roundID, "本轮交互结构无效，完整回复已保留；请直接回复或用聊天补充")
			return false
		}
		if report != nil && strings.TrimSpace(report.Question) != "" {
			return publishQuestion(st, c, roundID, sourceMessageID, legacyQuestion(report.Question))
		}
		return false
	}
	// Final safety net: even if streaming deltas were missed, a valid block is
	// never shown as raw JSON after settle.
	if visible != text {
		for i := range c.Messages {
			if c.Messages[i].ID == sourceMessageID {
				c.Messages[i].DisplayText = &visible
				st.Conversations[c.ID] = c
				break
			}
		}
	}
	return publishQuestion(st, c, roundID, sourceMessageID, block.Definition)
}

func markInteractionError(st *workbench.State, conversationID, roundID, reason string) {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return
	}
	for i := range c.Rounds {
		if c.Rounds[i].ID == roundID {
			c.Rounds[i].InteractionError = reason
			st.Conversations[c.ID] = c
			return
		}
	}
}

func legacyQuestion(question string) workbench.InteractionDefinition {
	question = strings.TrimSpace(question)
	if len([]rune(question)) > workbench.MaxInteractionTextarea {
		question = string([]rune(question)[:workbench.MaxInteractionTextarea])
	}
	max := workbench.MaxInteractionTextarea
	return workbench.InteractionDefinition{
		Title:       "请补充信息",
		Description: question,
		AllowCancel: true,
		Fields: []workbench.InteractionField{{
			ID:        "answer",
			Type:      workbench.FieldTextarea,
			Label:     "你的回答",
			Required:  true,
			MaxLength: &max,
		}},
	}
}

func publishQuestion(st *workbench.State, c workbench.Conversation, roundID, sourceMessageID string, def workbench.InteractionDefinition) bool {
	now := time.Now().UTC().Format(time.RFC3339)
	dedupe := workbench.SourceAssistantQuestion + ":" + roundID
	sum := sha256.Sum256([]byte(c.ID + "\x00" + dedupe))
	req := workbench.InteractionRequest{
		SchemaVersion:   workbench.InteractionSchemaVersion,
		Version:         1,
		ID:              "ix_" + hex.EncodeToString(sum[:12]),
		ProjectID:       c.ProjectID,
		ConversationID:  c.ID,
		TaskID:          c.TaskID,
		RoundID:         roundID,
		SourceMessageID: sourceMessageID,
		Source:          workbench.SourceAssistantQuestion,
		Title:           def.Title,
		Description:     def.Description,
		Fields:          def.Fields,
		Blocking:        true,
		// An ordinary question has no upstream waiter: local cancellation is
		// always honest, so the user can always dismiss a stale question
		// instead of being trapped behind the delivery gate.
		AllowCancel:       true,
		AllowTextFallback: true,
		Status:            workbench.InteractionPending,
		CreatedAt:         now,
		DedupeKey:         dedupe,
	}
	stored, _, err := workbench.PublishInteraction(st, req)
	if err != nil {
		// A failed publish must never let automation continue as if the round
		// produced a reviewable result.
		markInteractionError(st, c.ID, roundID, "交互请求未能保存，自动流程已暂停")
		return false
	}
	// A repeated settle of the same round must not resurrect an answered
	// request as a fresh waiting state.
	return stored.Status == workbench.InteractionPending
}

// flushSettledInteractionAnswers sends answers that were recorded while this
// run was still active. It runs only after the round boundary released the
// manager lock, and a proven-unsent start is reverted so the answer can be
// retried without losing it.
func (m *Manager) flushSettledInteractionAnswers(conversationID string) {
	c := m.store.Snapshot().Conversations[conversationID]
	if c.ID == "" || c.RunState != "idle" {
		return
	}
	messageID, err := m.store.FlushInteractionAnswers(conversationID)
	if err != nil || messageID == "" {
		return
	}
	if err := m.begin(conversationID, messageID); err != nil {
		_ = m.store.Update(func(st *workbench.State) error {
			v := st.Conversations[conversationID]
			v.RunState = "idle"
			v.Error = "答复已记录，原会话未能启动；请重试发送答复"
			for i := range v.Messages {
				if v.Messages[i].ID == messageID && v.Messages[i].Delivery == "pending" {
					v.Messages[i].Delivery = "not_sent"
				}
			}
			for i := range v.Rounds {
				if v.Rounds[i].ID == messageID && v.Rounds[i].Phase == "running" {
					v.Rounds[i].Phase = "interrupted"
					v.Rounds[i].FinishedAt = time.Now().UTC().Format(time.RFC3339)
				}
			}
			st.Conversations[conversationID] = v
			workbench.SyncConversationInteractionsFromMessages(st, conversationID)
			return nil
		})
	}
}

// awaitUser separates "waiting for a human answer" from "finished result".
// Nothing is confirmed or delivered while the request is open.
func awaitUser(st *workbench.State, conversationID string) {
	t, x, ok := executionFor(*st, conversationID)
	if !ok {
		return
	}
	x.Phase = "awaiting_user"
	x.Error = ""
	x.FinishedAt = ""
	x.Summary = ""
	t.Status = "waiting_user"
	st.Executions[x.ID] = x
	st.Tasks[t.ID] = t
}
