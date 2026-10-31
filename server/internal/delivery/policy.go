package delivery

import (
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// gateCode re-reads the live policy before every side-effect stage. The
// frozen snapshot stays untouched: authorization revocation or a change of
// the protected scope must stop external writes between stages, not replay
// the original grant.
func (m *Manager) gateCode(d workbench.Delivery) (code, reason string) {
	current, err := m.store.DeliveryPolicy(d.ProjectID)
	if err != nil {
		return "policy_invalid", "项目交付配置不可读"
	}
	if !current.Enabled {
		return "authorization_disabled", "自动交付授权未启用"
	}
	if !current.RevokedAt.IsZero() {
		return "authorization_revoked", "自动交付授权已撤销"
	}
	if err := workbench.ValidateDeliveryPolicy(current); err != nil {
		return "policy_invalid", err.Error()
	}
	if workbench.ProtectedDeliveryFingerprint(current) != workbench.ProtectedDeliveryFingerprint(d.Policy) {
		return "policy_changed", "项目交付配置已变化，请重新确认授权"
	}
	return m.gateWorkflowCommits(d)
}

// executionOf resolves the task/project/execution triple of one delivery.
func executionOf(st workbench.State, d workbench.Delivery) (p workbench.Project, t workbench.Task, x workbench.Execution, ok bool) {
	t, ok = st.Tasks[d.TaskID]
	if !ok {
		return
	}
	p, ok = st.Projects[t.ProjectID]
	if !ok {
		return
	}
	x, ok = st.Executions[t.ExecutionID]
	return
}

// roundSettled reports whether the cycle's dependency round has definitely
// finished (or was confirmed by a human). A round that asked a blocking
// question settles through the round created from its answers, so a delivery
// never resumes on a half-answered form.
func (m *Manager) roundSettled(st workbench.State, d workbench.Delivery) (settled, found bool) {
	_, settled, code, _ := workbench.DeliveryDependency(st, d.TaskID, d.RoundID)
	if code == "round_missing" {
		return false, false
	}
	return settled, true
}
