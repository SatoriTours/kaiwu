package agent

import (
	"context"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// DispatchRepair claims one bounded repair job and sends its system prompt to
// the delivery's original conversation. Claim is the idempotent admission; a
// repeated dispatch of the same job never re-charges the budget and never
// sends a second request. Send failure leaves the job dispatching so the
// repair loop (delivery manager) can analyze or re-drive it later; no blind
// resend happens here.
func (m *Manager) DispatchRepair(ctx context.Context, deliveryID, jobID string) error {
	repair, ok, err := m.store.ClaimDeliveryRepair(jobID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err = m.store.MarkRepairDispatching(jobID); err != nil {
		return err
	}
	d, err := m.store.Delivery(deliveryID)
	if err != nil {
		return err
	}
	t, ok := m.store.Snapshot().Tasks[d.TaskID]
	if !ok || t.ConversationID == "" {
		return workbench.Missing()
	}
	return m.Send(t.ConversationID, repair.MessageID)
}
