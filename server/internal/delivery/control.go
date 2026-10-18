package delivery

import (
	"context"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Accept records the explicit, token-bound acceptance of one completed
// delivery cycle. The store owns the contract; the manager exposes it so
// callers never touch store internals directly.
func (m *Manager) Accept(ctx context.Context, operationID, deliveryID, token string, expectedVersion uint64) (workbench.Receipt, error) {
	_ = ctx
	return m.store.AcceptDelivery(operationID, deliveryID, token, expectedVersion)
}

// Control applies pause/resume/cancel/extend_budget to one delivery cycle.
// Cancel verifies the writer first: while any writer occupies the task (or
// the owner lock refuses the check), the cycle must not terminate.
func (m *Manager) Control(ctx context.Context, operationID, deliveryID, action string, expectedVersion uint64) (workbench.Receipt, error) {
	if action == "cancel" {
		lease, err := m.cfg.Agent.AcquireDeliveryLease(ctx, deliveryID, "task")
		if err != nil {
			return workbench.Receipt{}, workbench.Conflict("写入者核查未通过，不能终止交付")
		}
		_ = lease.Release(true)
	}
	r, err := m.store.ControlDelivery(operationID, deliveryID, action, "", expectedVersion)
	if err != nil {
		return r, err
	}
	if action == "pause" || action == "cancel" {
		m.dropLease(deliveryID, false)
	}
	return r, nil
}
