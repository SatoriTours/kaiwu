//go:build !linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func PrepareRunMaterial(context.Context, string, executionenv.Identity, string) (*RunMaterial, error) {
	return nil, executionenv.ErrUnavailable
}
