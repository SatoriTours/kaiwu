package delivery

import (
	"context"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
)

// Clock abstracts time so deadlines come from persisted records and tests can
// advance deterministically without real sleeps.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the production clock: real time and real timers. Deadlines
// still come from persisted records; the clock only supplies "now".
type SystemClock struct{}

func (SystemClock) Now() time.Time                         { return time.Now() }
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// AgentPort is the narrow agent surface the delivery worker may use: sends
// for the cycle's own conversation, Task 6 write leases and the Task 8
// bounded repair dispatch. It never lets delivery import the agent
// manager's runtime details.
type AgentPort interface {
	Send(conversationID, messageID string) error
	AcquireDeliveryLease(ctx context.Context, deliveryID, scope string) (agent.DeliveryLease, error)
	DispatchRepair(ctx context.Context, deliveryID, jobID string) error
}

// Config wires one delivery manager. All ports are required; the transport
// stack stays read-only by construction.
type Config struct {
	Agent   AgentPort
	Actions *githubactions.Client
	Remote  *gitops.RemoteRunner
	Clock   Clock
	DataDir string
}

// Preflight is the read-only readiness report of a project's automated
// delivery setup. It never commits or pushes; blockers only state what a
// machine may not prove about write access.
type Preflight struct {
	Ready       bool     `json:"ready"`
	Fingerprint string   `json:"fingerprint"`
	Blockers    []string `json:"blockers"`
}
