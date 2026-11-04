//go:build !linux

package executionenv

// No host fallback on unsupported platforms.
func NewSystemdInspector(string) (*SystemdInspector, error) { return nil, ErrUnavailable }
