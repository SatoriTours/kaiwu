package workbench

import "testing"

func TestDeliveryCandidateWritesRequirePause(t *testing.T) {
	for _, phase := range []string{"developing", "task_ci", "awaiting_acceptance", "integrating", "main_ci", "blocked", "needs_check", "paused"} {
		t.Run(phase, func(t *testing.T) {
			s, p, task := deliveryFixture(t)
			enableDelivery(t, s, p)
			r := startDelivery(t, s, p, task)
			d, err := s.Delivery(r.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			d.Phase = phase
			if _, err = s.AdvanceDelivery(d.ID, d.Version, d, nil); err != nil {
				t.Fatal(err)
			}
			conv := s.Snapshot().Tasks[task].ConversationID
			if err = s.Update(func(st *State) error {
				c := st.Conversations[conv]
				c.RunState = "idle"
				st.Conversations[conv] = c
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := len(s.Snapshot().Conversations[conv].Messages)
			_, err = s.Command(Command{OperationID: "new-write", Revision: s.Snapshot().Revision, Type: "send_message", ProjectID: p, ConversationID: conv, Text: "继续修改候选"})
			if phase == "paused" {
				if err != nil {
					t.Fatalf("paused task cannot be edited: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s admitted a competing writer without pausing delivery", phase)
			}
			if len(s.Snapshot().Conversations[conv].Messages) != before {
				t.Fatal("refused write left a message behind")
			}
		})
	}
}
