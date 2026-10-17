//go:build !linux

package agent

import (
	"errors"
	"os"
)

type executionLease struct{}

func (*executionLease) inheritedFile() *os.File { return nil }

func acquireExecutionLease(string) (*executionLease, error) {
	return nil, errors.New("execution is unsupported")
}
func (*executionLease) attempt() error     { return errors.New("execution is unsupported") }
func (*executionLease) started(int) error  { return errors.New("execution is unsupported") }
func (*executionLease) unused() bool       { return true }
func (*executionLease) release(bool) error { return nil }

func acquireProjectMergeLease(string) (*executionLease, error) {
	return nil, errors.New("merge is unsupported")
}

func processGroupGone(int) bool { return false }
