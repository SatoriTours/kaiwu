//go:build !linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func (m *RunMaterial) Listen(context.Context) (*ControlListener, error) {
	return nil, executionenv.ErrUnavailable
}
