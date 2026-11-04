//go:build !linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"os"
)

func sendRetained(context.Context, *BridgeChannel, []*os.File) error {
	return executionenv.ErrUnavailable
}
func receiveRetained(context.Context, *HostChannel, BundleExpectation) (*Bundle, error) {
	return nil, executionenv.ErrUnavailable
}
