//go:build !linux

package pirpc

import (
	"errors"
	"os/exec"
)

func ProcessGroupsSupported() bool { return false }
func configureProcessGroup(cmd *exec.Cmd, owned bool) error {
	if owned {
		return errors.New("owned execution groups are not supported on this platform")
	}
	return nil
}
func stopProcessGroup(pid int) error { return errors.New("unsupported process group") }
func retireProcessGroup(pid int, stop func() error) error {
	return errors.New("unsupported process group")
}
