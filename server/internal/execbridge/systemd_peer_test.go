package execbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func TestSystemdPeerInspectorRequiresNativeBackend(t *testing.T) {
	var inspector UnitPeerInspector = SystemdPeerInspector{}
	if state, err := inspector.InspectBridge(context.Background(), executionenv.Identity{}); !errors.Is(err, executionenv.ErrUnavailable) || state.MainPID != 0 || state.Observation.State != "" {
		t.Fatal("nil backend manufactured ownership")
	}
}
