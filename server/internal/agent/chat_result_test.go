package agent

import (
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"testing"
)

func TestOrdinaryAnswerPreservesPreviousDevelopmentResult(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		phase := "finished"
		if confirmed {
			phase = "confirmed"
		}
		st := workbench.State{
			Tasks:         map[string]workbench.Task{"t": {ID: "t", ConversationID: "c", ExecutionID: "x", Status: "running"}},
			Executions:    map[string]workbench.Execution{"x": {ID: "x", Phase: "running", Summary: "old", Tools: []workbench.ToolRecord{{ID: "old-tool"}}}},
			Conversations: map[string]workbench.Conversation{"c": {ID: "c", TaskID: "t", Messages: []workbench.Message{{ID: "dev", Role: "user"}, {ID: "dev_reply", Role: "assistant", Text: "developed"}, {ID: "qa", Role: "user"}, {ID: "qa_reply", Role: "assistant", Text: "explanation"}}, Rounds: []workbench.Round{{ID: "dev", Phase: phase, FinishedAt: "original-finish", Tools: []workbench.ToolRecord{{ID: "old-tool"}}}, {ID: "qa", Phase: "finished", FinishedAt: "answer-finish", Tools: []workbench.ToolRecord{}}}}},
		}
		settleExecution(&st, "c", "qa_reply", false)
		c := st.Conversations["c"]
		if c.Rounds[1].Phase != "answered" {
			t.Fatalf("ordinary answer became a new development result: %+v", c.Rounds)
		}
		x := st.Executions["x"]
		if x.Summary != "developed" || x.FinishedAt != "original-finish" {
			t.Fatalf("result overwritten: %+v", x)
		}
		want := "needs_confirmation"
		if confirmed {
			want = "completed"
		}
		if st.Tasks["t"].Status != want {
			t.Fatalf("status: %s", st.Tasks["t"].Status)
		}
	}
}
