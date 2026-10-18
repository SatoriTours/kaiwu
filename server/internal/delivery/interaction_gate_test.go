package delivery

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// askQuestion simulates the original pi manager settling the cycle's round
// with one blocking ordinary question: the round waits for the user, the task
// leaves the result state, and the request is persisted.
func (m *machine) askQuestion(roundID string) workbench.InteractionRequest {
	m.t.Helper()
	// A real repairing agent usually has already edited the workspace before
	// it asks. Represent that work so the post-answer failure is a genuinely
	// new candidate rather than the identical-failure repair_loop case.
	if m.tree != "" {
		writeFile(m.t, filepath.Join(m.tree, "question-"+roundID[:8]+".txt"), "修复轮提问前的实际修改\n")
	}
	var out workbench.InteractionRequest
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.ProjectID = "p"
		c.RunState = "idle"
		for i := range c.Messages {
			if c.Messages[i].Delivery == "pending" || c.Messages[i].Delivery == "accepted" {
				c.Messages[i].Delivery = "complete"
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for i := range c.Rounds {
			if c.Rounds[i].ID == roundID {
				c.Rounds[i].Phase = "awaiting_user"
				c.Rounds[i].FinishedAt = now
			}
		}
		st.Conversations["c"] = c
		t := st.Tasks[m.taskID]
		t.Status = "waiting_user"
		st.Tasks[t.ID] = t
		x := st.Executions[t.ExecutionID]
		x.Phase = "awaiting_user"
		x.FinishedAt = ""
		x.Summary = ""
		st.Executions[x.ID] = x
		created, _, err := workbench.PublishInteraction(st, workbench.InteractionRequest{
			SchemaVersion:     workbench.InteractionSchemaVersion,
			ID:                "ix_question_" + roundID[:12],
			Version:           1,
			ProjectID:         "p",
			ConversationID:    "c",
			TaskID:            m.taskID,
			RoundID:           roundID,
			Source:            workbench.SourceAssistantQuestion,
			Title:             "需要确认",
			Fields:            []workbench.InteractionField{{ID: "allow", Type: workbench.FieldConfirm, Label: "是否允许继续", Required: true}},
			Blocking:          true,
			AllowCancel:       true,
			AllowTextFallback: true,
			Status:            workbench.InteractionPending,
			CreatedAt:         now,
			DedupeKey:         "assistant_question:" + roundID,
		})
		out = created
		return err
	}); e != nil {
		m.t.Fatal(e)
	}
	return out
}

func (m *machine) waitWaiting(code string) workbench.Delivery {
	m.t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		m.clock.advance(time.Millisecond)
		d, e := m.store.Delivery(m.deliveryID)
		if e != nil {
			m.t.Fatal(e)
		}
		if d.WaitingCode == code {
			return d
		}
		if d.Phase == "blocked" {
			m.t.Fatalf("waiting became terminal: %s %s", d.BlockCode, d.BlockReason)
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.t.Fatalf("delivery never reported waiting code %q", code)
	return workbench.Delivery{}
}

// TestDeliveryWaitsForUserAnswerAndContinuesSameCycle is the regression for
// treating a blocking question as a terminal delivery block: the same cycle,
// authorization snapshot and candidate checks must continue after the answer
// and the dependent round finish, without a second dispatch.
func TestDeliveryWaitsForUserAnswerAndContinuesSameCycle(t *testing.T) {
	m := newMachine(t, true)
	d := m.waitPhase(t, "developing")
	fingerprint := workbench.ProtectedDeliveryFingerprint(d.Policy)
	if sends := m.agent.sent(); len(sends) != 1 {
		t.Fatalf("initial dispatch %v", sends)
	}
	question := m.askQuestion(d.RoundID)
	m.manager.Wake()
	waiting := m.waitWaiting("interaction_pending")
	if waiting.Phase != "developing" || waiting.ID != d.ID || waiting.Cycle != d.Cycle {
		t.Fatalf("cycle changed while waiting: %+v", waiting)
	}
	if len(m.agent.sent()) != 1 {
		t.Fatal("delivery kept dispatching while an answer was missing")
	}
	if waiting.Version <= d.Version {
		t.Fatal("waiting state was not persisted")
	}
	// Answer through the real store command path: exactly one aggregated turn
	// is created and bound to the cycle's dependency chain.
	r, e := m.store.Command(workbench.Command{OperationID: "answer-delivery-question", Revision: 0, Type: "answer_interaction", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: 1, Answers: map[string]any{"allow": true}})
	if e != nil {
		t.Fatal(e)
	}
	if r.CreatedID == "" {
		t.Fatal("answer produced no follow-up turn")
	}
	// The HTTP dispatcher would send this message; record it through the same
	// fake agent port the delivery worker uses.
	if e = m.agent.Send("c", r.CreatedID); e != nil {
		t.Fatal(e)
	}
	// The fake agent then completes the dependent round normally.
	m.finishRoundNamed(r.CreatedID, happyReport())
	m.clock.advance(time.Millisecond)
	resumed := m.waitPhase(t, "local_checking")
	if resumed.ID != d.ID || resumed.WaitingCode != "" {
		t.Fatalf("resumed cycle %+v", resumed)
	}
	if workbench.ProtectedDeliveryFingerprint(resumed.Policy) != fingerprint {
		t.Fatal("authorization snapshot changed across the wait")
	}
	if sends := m.agent.sent(); len(sends) != 2 {
		t.Fatalf("worker duplicated dispatch: %v", sends)
	}
	if resumed.BlockCode != "" {
		t.Fatalf("resumed with block code %q", resumed.BlockCode)
	}
}

// TestDeliveryCancelledQuestionStaysWaitingUntilNewUserTurn proves a local
// cancellation never releases delivery by itself.
func TestDeliveryCancelledQuestionStaysWaitingUntilNewUserTurn(t *testing.T) {
	m := newMachine(t, true)
	d := m.waitPhase(t, "developing")
	question := m.askQuestion(d.RoundID)
	m.manager.Wake()
	m.waitWaiting("interaction_pending")
	if _, e := m.store.Command(workbench.Command{OperationID: "cancel-delivery-question", Revision: 0, Type: "cancel_interaction", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: 1}); e != nil {
		t.Fatal(e)
	}
	m.manager.Wake()
	waiting := m.waitWaiting("interaction_unanswered")
	if waiting.Phase != "developing" {
		t.Fatalf("cancelled question advanced the cycle: %+v", waiting)
	}
	if len(m.agent.sent()) != 1 {
		t.Fatal("cancellation dispatched a delivery turn")
	}
	// Executable path: the user sends a new instruction that finishes a later
	// round; only then does the same cycle continue.
	message, e := m.store.Command(workbench.Command{OperationID: "continue-after-cancel", Revision: m.store.Snapshot().Revision, Type: "send_message", ProjectID: "p", ConversationID: "c", Text: "按默认方案继续"})
	if e != nil {
		t.Fatal("cancelled question blocked an explicit new instruction:", e)
	}
	m.finishRoundNamed(message.CreatedID, happyReport())
	m.clock.advance(time.Millisecond)
	resumed := m.waitPhase(t, "local_checking")
	if resumed.ID != d.ID || resumed.WaitingCode != "" {
		t.Fatalf("not resumed after explicit new turn: %+v", resumed)
	}
}
