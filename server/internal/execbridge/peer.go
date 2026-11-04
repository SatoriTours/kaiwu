package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type PeerState struct {
	Observation executionenv.Observation
	MainPID     int
}

// Implementations must honor ctx for all external inspection and cleanup.
// NativeVerifier receives the authentication entry point's shared budget.
type UnitPeerInspector interface {
	InspectBridge(context.Context, executionenv.Identity) (PeerState, error)
}
type NativeVerifier struct {
	Expected  executionenv.Identity
	Inspector UnitPeerInspector
}
