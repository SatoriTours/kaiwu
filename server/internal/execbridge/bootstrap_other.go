//go:build !linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func LoadPublicBootstrap(context.Context, string) (PublicBootstrap, error) {
	return PublicBootstrap{}, executionenv.ErrUnavailable
}
