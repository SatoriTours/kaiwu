package delivery

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// answerQuestion drives the real command path for one repair-round question,
// records the HTTP-equivalent dispatch, and finishes the answer round.
func (m *machine) answerQuestion(q workbench.InteractionRequest, advanceOnAnswer time.Duration) workbench.InteractionRequest {
	m.t.Helper()
	_, e := m.store.Command(workbench.Command{
		OperationID:    "answer-" + q.ID,
		Type:           "answer_interaction",
		ProjectID:      "p",
		ConversationID: "c",
		TaskID:         m.taskID,
		RequestID:      q.ID,
		RequestVersion: q.Version,
		AnswerMode:     "form",
		Answers:        map[string]any{"allow": true},
	})
	if e != nil {
		m.t.Fatalf("answer refused: %v", e)
	}
	st := m.store.Snapshot()
	var stored workbench.InteractionRequest
	for _, req := range st.Conversations["c"].Interactions {
		if req.ID == q.ID {
			stored = req
		}
	}
	if stored.Response == nil || stored.Response.FollowUpRoundID == "" {
		m.t.Fatalf("answer not linked to a follow-up round: %+v", stored.Response)
	}
	// While the answer round runs, the delivery must classify it as AI-active
	// (interaction_answering), not as user waiting.
	if advanceOnAnswer > 0 {
		m.waitWaiting("interaction_answering")
		m.clock.advance(advanceOnAnswer)
	}
	m.finishRoundIn("c", stored.Response.FollowUpRoundID, happyReport())
	return stored
}

// TestUserWaitDuringRepairIsExcludedFromActiveBudget is the D1 regression: a
// user thinking during a repair-round question must not consume the activation
// budget and terminate the cycle with repair_deadline.
func TestUserWaitDuringRepairIsExcludedFromActiveBudget(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	parked := m.mustDelivery()
	baseActive := parked.ActiveSince
	if parked.Phase != "repairing" {
		t.Fatalf("parked phase %q", parked.Phase)
	}
	// The wait start is persisted on the delivery row: a restart reads the
	// same segment instead of losing or restarting the accounting.
	if parked.WaitingSince.IsZero() {
		t.Fatal("waiting start was not persisted")
	}

	m.clock.advance(90 * time.Minute)
	m.answerQuestion(question, 0)
	m.waitForRepairClaims(2)
	d := m.mustDelivery()
	if d.Phase == "blocked" {
		t.Fatalf("user wait terminated the cycle: %q %s", d.BlockCode, d.BlockReason)
	}
	if !d.ActiveSince.Equal(baseActive) {
		t.Fatalf("activation base was reset: %v -> %v", baseActive, d.ActiveSince)
	}
	if d.WaitExcludedMillis < int64((89 * time.Minute).Milliseconds()) {
		t.Fatalf("user wait not excluded: %dms", d.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 2 {
		t.Fatalf("repair #2 not dispatched after the wait: %d", got)
	}
}

// TestAnswerRoundRuntimeStillCountsTowardActiveBudget proves the exclusion is
// limited to the user's own waiting: the answer round actually running must
// still consume the activation budget.
func TestAnswerRoundRuntimeStillCountsTowardActiveBudget(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(90 * time.Minute)
	// The answer round runs for 61 minutes of real AI activity; together with
	// the small active time before the question this exceeds the 3600s budget.
	m.answerQuestion(question, 61*time.Minute)
	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("wrong stop code %q (%s)", blocked.BlockCode, blocked.BlockReason)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("deadline must not dispatch another repair: %d", got)
	}
}

// TestRepeatedUserWaitsDoNotRenewTheBudget: two long waits are both excluded,
// but the active time consumed between them still accumulates to the deadline.
func TestRepeatedUserWaitsDoNotRenewTheBudget(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	first := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(90 * time.Minute)
	m.answerQuestion(first, 0)
	m.waitForRepairClaims(2)

	// 50 minutes of real activity during the second repair attempt.
	m.clock.advance(50 * time.Minute)
	second := m.askQuestion(m.repairRounds()[1].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(90 * time.Minute)
	// Another 12 active minutes while the answer round runs: 62 > 60.
	m.answerQuestion(second, 12*time.Minute)

	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("wrong stop code %q (%s)", blocked.BlockCode, blocked.BlockReason)
	}
	d := m.mustDelivery()
	if d.WaitExcludedMillis < int64((179 * time.Minute).Milliseconds()) {
		t.Fatalf("repeated waits not accumulated: %dms", d.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 2 {
		t.Fatalf("repeated waits renewed the budget: repairs=%d", got)
	}
}

// Manual pause/cancel are explicit user controls and must not be rewritten by
// an answer arriving later.
func TestManualPauseAndCancelSurviveLaterInteractionAnswer(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	d := m.mustDelivery()
	paused, e := m.store.ControlDelivery("pause-wait", d.ID, "pause", "", d.Version)
	if e != nil {
		t.Fatal(e)
	}
	_ = paused
	state := m.mustDelivery()
	if state.Phase != "paused" || state.WaitingCode != "interaction_pending" {
		t.Fatalf("pause rewritten: %+v", state)
	}
	cancelled, e := m.store.ControlDelivery("cancel-wait", state.ID, "cancel", "", state.Version)
	if e != nil {
		t.Fatal(e)
	}
	_ = cancelled
	state = m.mustDelivery()
	if state.Phase != "cancelled" {
		t.Fatalf("cancel not persisted: %q", state.Phase)
	}
	m.answerQuestion(question, 0)
	if after := m.mustDelivery(); after.Phase != "cancelled" {
		t.Fatalf("answer rewrote the cancelled cycle: %q", after.Phase)
	}
}

// TestActiveElapsedAccounting is the pure timing rule used by repair_deadline.
func TestActiveElapsedAccounting(t *testing.T) {
	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	// 10 active minutes, 90 user-wait minutes closed, 5 active minutes then an
	// open user wait: total 105 minutes, active 15.
	d := workbench.Delivery{ActiveSince: base, WaitExcludedMillis: int64((100 * time.Minute).Milliseconds())}
	if got := activeElapsed(d, base.Add(105*time.Minute)); got != 5*time.Minute {
		t.Fatalf("completed wait: %v", got)
	}
	d.WaitingCode = "interaction_pending"
	d.WaitingSince = base.Add(105 * time.Minute)
	if got := activeElapsed(d, base.Add(195*time.Minute)); got != 5*time.Minute {
		t.Fatalf("open wait: %v", got)
	}
	// The answering state is AI-active: it counts.
	d.WaitingCode = "interaction_answering"
	d.WaitingSince = time.Time{}
	if got := activeElapsed(d, base.Add(195*time.Minute)); got != 95*time.Minute {
		t.Fatalf("answering time must count: %v", got)
	}
	// A transition pending -> answering closes the wait exactly once.
	open := workbench.Delivery{ActiveSince: base, WaitingCode: "interaction_pending", WaitingSince: base.Add(10 * time.Minute)}
	closeUserWait(&open, base.Add(100*time.Minute))
	if open.WaitExcludedMillis != int64((90*time.Minute).Milliseconds()) || !open.WaitingSince.IsZero() {
		t.Fatalf("wait close accounting %+v", open)
	}
	closeUserWait(&open, base.Add(200*time.Minute))
	if open.WaitExcludedMillis != int64((90 * time.Minute).Milliseconds()) {
		t.Fatal("double-counted the same wait window")
	}
}

// cancelQuestion drives the real cancel path for one repair-round question.
func (m *machine) cancelQuestion(q workbench.InteractionRequest) {
	m.t.Helper()
	if _, e := m.store.Command(workbench.Command{OperationID: "cancel-" + q.ID, Type: "cancel_interaction", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: q.ID, RequestVersion: q.Version}); e != nil {
		m.t.Fatalf("cancel refused: %v", e)
	}
}

// TestCancelledQuestionNewRoundRuntimeStillCounts is D2: while the cycle is
// parked in interaction_unanswered, the user's new instruction round is real
// AI activity and must consume the activation budget.
func TestCancelledQuestionNewRoundRuntimeStillCounts(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.CISeconds = 7 * 3600
	})
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(5 * time.Minute)
	m.cancelQuestion(question)
	m.manager.Wake()
	waiting := m.waitWaiting("interaction_unanswered")
	if waiting.WaitingSince.IsZero() {
		t.Fatal("unanswered wait lost its start")
	}

	turn, e := m.store.Command(workbench.Command{OperationID: "continue-after-cancel", Revision: m.store.Snapshot().Revision, Type: "send_message", ProjectID: "p", ConversationID: "c", Text: "按默认方案继续，不要再次询问"})
	if e != nil {
		t.Fatal(e)
	}
	// 62 minutes of new-round AI runtime, indistinguishable from a wall-clock
	// jump: the hook timestamp must end the user wait at the turn start.
	m.clock.advance(62 * time.Minute)
	m.finishRoundIn("c", turn.CreatedID, happyReport())
	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("new-round AI runtime was excluded as user wait: %q (%s) waitExcluded=%dms", blocked.BlockCode, blocked.BlockReason, blocked.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("repair dispatched despite exhausted budget: %d", got)
	}
}

// TestCancelledQuestionShortNewRoundContinues: a short new round after a
// cancellation still lets the same cycle continue to repair #2.
func TestCancelledQuestionShortNewRoundContinues(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(5 * time.Minute)
	m.cancelQuestion(question)
	m.manager.Wake()
	m.waitWaiting("interaction_unanswered")
	turn, e := m.store.Command(workbench.Command{OperationID: "short-after-cancel", Revision: m.store.Snapshot().Revision, Type: "send_message", ProjectID: "p", ConversationID: "c", Text: "简短补充"})
	if e != nil {
		t.Fatal(e)
	}
	m.clock.advance(10 * time.Minute)
	m.finishRoundIn("c", turn.CreatedID, happyReport())
	m.waitForRepairClaims(2)
	d := m.mustDelivery()
	if d.Phase == "blocked" {
		t.Fatalf("short round wrongly stopped the cycle: %q", d.BlockCode)
	}
	if d.WaitExcludedMillis < int64((4*time.Minute).Milliseconds()) || d.WaitExcludedMillis > int64((16*time.Minute).Milliseconds()) {
		t.Fatalf("wait exclusion not limited to real waiting: %dms", d.WaitExcludedMillis)
	}
}

// failInteractionDispatch simulates a proven-unsent answer dispatch.
func (m *machine) failInteractionDispatch(messageID string) {
	m.t.Helper()
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.RunState = "failed"
		c.Error = "fixture: dispatch failed"
		for i := range c.Messages {
			if c.Messages[i].ID == messageID {
				c.Messages[i].Delivery = "not_sent"
			}
		}
		for i := range c.Interactions {
			if c.Interactions[i].Response != nil && c.Interactions[i].Response.MessageID == messageID {
				c.Interactions[i].Response.Delivery = workbench.DeliveryNotSent
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		for i := range c.Rounds {
			if c.Rounds[i].ID == messageID {
				c.Rounds[i].Phase = "failed"
				c.Rounds[i].FinishedAt = now
			}
		}
		st.Conversations["c"] = c
		t := st.Tasks[m.taskID]
		t.Status = "failed"
		st.Tasks[t.ID] = t
		if t.ExecutionID != "" {
			x := st.Executions[t.ExecutionID]
			x.Phase = "failed"
			x.Error = "fixture: dispatch failed"
			st.Executions[x.ID] = x
		}
		return nil
	}); e != nil {
		m.t.Fatal(e)
	}
}

// TestRetriedAnswerRuntimeStillCounts: the retry of a proven-unsent answer
// starts a real run; its runtime must count, while the waiting before the
// retry stays excluded.
func TestRetriedAnswerRuntimeStillCounts(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.CISeconds = 7 * 3600
	})
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(5 * time.Minute)
	if _, e := m.store.Command(workbench.Command{OperationID: "retry-base-" + question.ID, Type: "answer_interaction", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: question.Version, Answers: map[string]any{"allow": true}}); e != nil {
		t.Fatal(e)
	}
	var stored workbench.InteractionRequest
	for _, req := range m.store.Snapshot().Conversations["c"].Interactions {
		if req.ID == question.ID {
			stored = req
		}
	}
	if stored.Response == nil || stored.Response.FollowUpRoundID == "" {
		t.Fatalf("answer did not create a follow-up round: %+v", stored.Response)
	}
	m.failInteractionDispatch(stored.Response.MessageID)
	m.manager.Wake()
	m.waitWaiting("interaction_unanswered")

	// The user waits 30 minutes before retrying.
	m.clock.advance(30 * time.Minute)
	if _, e := m.store.Command(workbench.Command{OperationID: "retry-answer-" + question.ID, Type: "retry_interaction_answer", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: question.Version}); e != nil {
		t.Fatalf("retry refused: %v", e)
	}
	// The retried run lasts 62 minutes: real AI activity over the budget.
	m.clock.advance(62 * time.Minute)
	m.finishRoundIn("c", stored.Response.FollowUpRoundID, happyReport())
	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("retried run was excluded as user wait: %q waitExcluded=%dms", blocked.BlockCode, blocked.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("repair dispatched despite exhausted budget: %d", got)
	}
}

// TestRetriedAnswerShortRunContinues: a short retried run still continues the
// same cycle to repair #2.
func TestRetriedAnswerShortRunContinues(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	question := m.askQuestion(m.repairRounds()[0].ID)
	m.waitWaiting("interaction_pending")
	m.clock.advance(5 * time.Minute)
	if _, e := m.store.Command(workbench.Command{OperationID: "retry-short-base-" + question.ID, Type: "answer_interaction", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: question.Version, Answers: map[string]any{"allow": true}}); e != nil {
		t.Fatal(e)
	}
	var stored workbench.InteractionRequest
	for _, req := range m.store.Snapshot().Conversations["c"].Interactions {
		if req.ID == question.ID {
			stored = req
		}
	}
	m.failInteractionDispatch(stored.Response.MessageID)
	m.manager.Wake()
	m.waitWaiting("interaction_unanswered")
	m.clock.advance(20 * time.Minute)
	if _, e := m.store.Command(workbench.Command{OperationID: "retry-short-" + question.ID, Type: "retry_interaction_answer", ProjectID: "p", ConversationID: "c", TaskID: m.taskID, RequestID: question.ID, RequestVersion: question.Version}); e != nil {
		t.Fatal(e)
	}
	m.clock.advance(10 * time.Minute)
	m.finishRoundIn("c", stored.Response.FollowUpRoundID, happyReport())
	m.waitForRepairClaims(2)
	d := m.mustDelivery()
	if d.Phase == "blocked" {
		t.Fatalf("short retried run wrongly stopped the cycle: %q", d.BlockCode)
	}
}

// publishNativeQuestion simulates pi blocking on an extension dialog in the
// middle of a running round (native requests never become awaiting_user).
func (m *machine) publishNativeQuestion(roundID, suffix string) workbench.InteractionRequest {
	m.t.Helper()
	// The repairing agent has already edited the workspace before asking; the
	// post-answer failure is then a genuinely new candidate.
	if m.tree != "" {
		writeFile(m.t, filepath.Join(m.tree, "native-question-"+suffix+".txt"), "原生提问前的实际修改\n")
	}
	var out workbench.InteractionRequest
	if e := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.ProjectID = "p"
		c.RunState = "running"
		st.Conversations["c"] = c
		created, _, err := workbench.PublishInteraction(st, workbench.InteractionRequest{
			SchemaVersion:  workbench.InteractionSchemaVersion,
			ID:             "ix_native_" + suffix,
			Version:        1,
			ProjectID:      "p",
			ConversationID: "c",
			TaskID:         m.taskID,
			RoundID:        roundID,
			Source:         workbench.SourceAgentUI,
			Title:          "原生确认 " + suffix,
			Fields:         []workbench.InteractionField{{ID: "confirmed", Type: workbench.FieldConfirm, Label: "请确认", Required: true}},
			Blocking:       true,
			AllowCancel:    true,
			Status:         workbench.InteractionPending,
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
			Native:         &workbench.InteractionNative{Method: "confirm", UpstreamID: "upstream_" + suffix, RunID: "run_" + suffix, Generation: 1},
			DedupeKey:      "agent_ui:run_" + suffix + ":upstream_" + suffix,
		})
		out = created
		return err
	}); e != nil {
		m.t.Fatal(e)
	}
	return out
}

// answerNative records the native answer and simulates the raw response having
// actually been written to pi (the agent manager calls NotifyUserTurn there).
func (m *machine) answerNative(q workbench.InteractionRequest, advanceAfter time.Duration) {
	m.t.Helper()
	if _, e := m.store.Command(workbench.Command{
		OperationID:    "native-answer-" + q.ID,
		Type:           "answer_interaction",
		ProjectID:      "p",
		ConversationID: "c",
		TaskID:         m.taskID,
		RequestID:      q.ID,
		RequestVersion: q.Version,
		AnswerMode:     "form",
		Answers:        map[string]any{"confirmed": false},
	}); e != nil {
		m.t.Fatal(e)
	}
	m.store.NotifyUserTurn(m.taskID, "c")
	if advanceAfter > 0 {
		m.clock.advance(advanceAfter)
	}
}

// TestNativeDialogUserThinkIsExcluded is D3: while pi is blocked on a native
// dialog the user's thinking must not drain the activation budget.
func TestNativeDialogUserThinkIsExcluded(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.CISeconds = 7 * 3600
	})
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	round := m.repairRounds()[0].ID
	question := m.publishNativeQuestion(round, "one")
	m.manager.Wake()
	parked := m.waitWaiting("interaction_pending")
	base := parked.ActiveSince
	m.clock.advance(90 * time.Minute)
	m.answerNative(question, 0)
	m.finishRoundIn("c", round, happyReport())
	m.waitForRepairClaims(2)
	d := m.mustDelivery()
	if d.Phase == "blocked" {
		t.Fatalf("native-dialog user think terminated the cycle: %q (%s)", d.BlockCode, d.BlockReason)
	}
	if !d.ActiveSince.Equal(base) {
		t.Fatal("activation base was reset")
	}
	if d.WaitExcludedMillis < int64((89 * time.Minute).Milliseconds()) {
		t.Fatalf("native user wait not excluded: %dms", d.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 2 {
		t.Fatalf("repair #2 not dispatched: %d", got)
	}
}

// TestNativeAnswerThenLongRunHitsDeadline: after the native response is
// actually written, the AI runtime counts, even if the worker observes late.
func TestNativeAnswerThenLongRunHitsDeadline(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.CISeconds = 7 * 3600
	})
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	round := m.repairRounds()[0].ID
	question := m.publishNativeQuestion(round, "long")
	m.manager.Wake()
	m.waitWaiting("interaction_pending")
	m.clock.advance(90 * time.Minute)
	// The response is written; the agent then runs for 62 minutes on the same
	// round. The user wait must have ended at the response moment.
	m.answerNative(question, 62*time.Minute)
	m.finishRoundIn("c", round, happyReport())
	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("native answer runtime not counted: %q", blocked.BlockCode)
	}
	if blocked.WaitExcludedMillis > int64((91 * time.Minute).Milliseconds()) {
		t.Fatalf("AI runtime was folded into the native user wait: %dms", blocked.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("repair dispatched despite deadline: %d", got)
	}
}

// TestNativeShortRunAfterAnswerContinues: the same flow with a short AI run
// continues the original cycle.
func TestNativeShortRunAfterAnswerContinues(t *testing.T) {
	m := newMachine(t, true)
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	round := m.repairRounds()[0].ID
	question := m.publishNativeQuestion(round, "short")
	m.manager.Wake()
	m.waitWaiting("interaction_pending")
	m.clock.advance(90 * time.Minute)
	m.answerNative(question, 10*time.Minute)
	m.finishRoundIn("c", round, happyReport())
	m.waitForRepairClaims(2)
	if d := m.mustDelivery(); d.Phase == "blocked" {
		t.Fatalf("short native run stopped the cycle: %q", d.BlockCode)
	}
}

// TestRepeatedNativeQuestionsDoNotRenewBudget: two native dialogs in one round
// both exclude their own user waits, but the AI time between them and after
// the last answer still accumulates.
// waitExcludedAtLeast waits until the delivery has actually accumulated the
// given user-wait duration. The original test only waited for the same
// WaitingCode, which is already true for the first wait; it therefore could
// advance the fake clock before the worker had closed the previous segment.
func (m *machine) waitExcludedAtLeast(min time.Duration) workbench.Delivery {
	m.t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		// Force a pass: the delivery worker wakes on store revisions, but a
		// coalesced/late wake must not turn this synchronization into a flaky
		// timeout. Wake is a test-side nudge, not a product behavior change.
		m.manager.Wake()
		m.clock.advance(time.Millisecond)
		d, e := m.store.Delivery(m.deliveryID)
		if e != nil {
			m.t.Fatal(e)
		}
		if d.WaitExcludedMillis >= min.Milliseconds() {
			return d
		}
		if d.Phase == "blocked" {
			m.t.Fatalf("waiting became terminal before exclusion reached %v: %s %s", min, d.BlockCode, d.BlockReason)
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.t.Fatalf("wait exclusion never reached %v", min)
	return workbench.Delivery{}
}

// waitNewWaitSegment waits for a wait segment that opened at or after `after`.
// Checking only the code cannot prove the previous segment was closed and a
// new one started.
func (m *machine) waitNewWaitSegment(code string, after time.Time) workbench.Delivery {
	m.t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		m.manager.Wake()
		m.clock.advance(time.Millisecond)
		d, e := m.store.Delivery(m.deliveryID)
		if e != nil {
			m.t.Fatal(e)
		}
		if d.WaitingCode == code && !d.WaitingSince.IsZero() && !d.WaitingSince.Before(after) {
			return d
		}
		if d.Phase == "blocked" {
			m.t.Fatalf("waiting became terminal before a new %q segment opened: %s %s", code, d.BlockCode, d.BlockReason)
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.t.Fatalf("delivery never opened a new %q segment after %v", code, after)
	return workbench.Delivery{}
}

func TestRepeatedNativeQuestionsDoNotRenewBudget(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.CISeconds = 7 * 3600
	})
	m.ci.setOpen(false, true, false, false)
	m.ci.setFailedJob(true, "Error: undefined variable app broken")
	m.finishRound(happyReport())

	m.waitForRepairClaims(1)
	round := m.repairRounds()[0].ID
	first := m.publishNativeQuestion(round, "rep1")
	m.manager.Wake()
	parked := m.waitWaiting("interaction_pending")
	baseActive := parked.ActiveSince
	m.clock.advance(90 * time.Minute)
	m.answerNative(first, 50*time.Minute)

	// Synchronize the first segment close before starting the second question:
	// the original test only observed the (already true) waiting code and could
	// advance the clock while the worker still held the first segment.
	firstClosed := m.waitExcludedAtLeast(89 * time.Minute)
	if firstClosed.WaitExcludedMillis > int64((91 * time.Minute).Milliseconds()) {
		t.Fatalf("first native wait over-excluded: %dms", firstClosed.WaitExcludedMillis)
	}
	secondAt := m.clock.Now()
	second := m.publishNativeQuestion(round, "rep2")
	m.manager.Wake()
	segment := m.waitNewWaitSegment("interaction_pending", secondAt)
	if segment.WaitExcludedMillis < int64((89 * time.Minute).Milliseconds()) {
		t.Fatalf("second segment opened without the first wait retained: %dms", segment.WaitExcludedMillis)
	}
	m.clock.advance(90 * time.Minute)
	m.answerNative(second, 12*time.Minute)

	// Synchronize the second close: the two waits must be accumulated while the
	// 50+12 minutes of real AI runtime stay counted.
	m.waitExcludedAtLeast(179 * time.Minute)
	m.finishRoundIn("c", round, happyReport())

	blocked := waitBlock(t, m)
	if blocked.BlockCode != "repair_deadline" {
		t.Fatalf("repeated native waits renewed the budget: %q (%s)", blocked.BlockCode, blocked.BlockReason)
	}
	d := m.mustDelivery()
	if !d.ActiveSince.Equal(baseActive) {
		t.Fatalf("activation base was reset: %v -> %v", baseActive, d.ActiveSince)
	}
	if d.WaitExcludedMillis < int64((179 * time.Minute).Milliseconds()) {
		t.Fatalf("native waits not accumulated: %dms", d.WaitExcludedMillis)
	}
	if d.WaitExcludedMillis > int64((181 * time.Minute).Milliseconds()) {
		t.Fatalf("AI runtime was folded into the native waits: %dms", d.WaitExcludedMillis)
	}
	if got := m.agent.repairsCount(); got != 1 {
		t.Fatalf("repair dispatched despite exhausted budget: %d", got)
	}
}
