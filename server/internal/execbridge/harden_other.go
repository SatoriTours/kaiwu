//go:build !linux

package execbridge

import (
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

func HardenBridge() error { return executionenv.ErrUnavailable }
