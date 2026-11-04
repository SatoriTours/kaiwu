//go:build !linux

package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"os"
)

func (c *HostChannel) SendBundle(context.Context, []*os.File, BundleExpectation) error {
	return executionenv.ErrUnavailable
}
func (c *BridgeChannel) ReceiveBundle(context.Context, BundleExpectation) (*Bundle, error) {
	return nil, executionenv.ErrUnavailable
}

func validateBundle([]*os.File, BundleExpectation) error { return executionenv.ErrUnavailable }

func (c *HostChannel) sendBundleLocked(context.Context, []*os.File, BundleExpectation) error {
	return executionenv.ErrUnavailable
}
func (c *BridgeChannel) receiveBundleLocked(context.Context, BundleExpectation) (*Bundle, error) {
	return nil, executionenv.ErrUnavailable
}
