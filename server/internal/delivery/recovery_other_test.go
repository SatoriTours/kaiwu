//go:build !linux

package delivery

import "testing"

// The real SIGKILL crash tests run a child process and inspect its signal
// death plus the post-crash process group, which the supported implementation
// only guarantees on Linux. Other platforms state that boundary instead of
// pretending coverage.
func TestDeliveryRecoveryCrashAndRestart(t *testing.T) {
	t.Skip("unsupported: real crash recovery requires Linux process-group semantics")
}
