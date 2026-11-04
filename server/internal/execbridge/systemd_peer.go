package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

// SystemdPeerInspector connects the native read-only ownership inspector to the
// socket's SO_PEERPIDFD pin. It is not a namespace/disk attestor or a launcher.
type SystemdPeerInspector struct {
	Inspector *executionenv.SystemdInspector
}

func (s SystemdPeerInspector) InspectBridge(ctx context.Context, expected executionenv.Identity) (PeerState, error) {
	if s.Inspector == nil {
		return PeerState{}, executionenv.ErrUnavailable
	}
	view, err := s.Inspector.Inspect(ctx, expected)
	if err != nil {
		return PeerState{}, err
	}
	return PeerState{Observation: view.Observation, MainPID: view.MainPID}, nil
}
