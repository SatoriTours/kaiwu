//go:build linux

package pirpc

import (
	"errors"
	"os/exec"
	"syscall"
	"unsafe"
)

func ProcessGroupsSupported() bool { return true }
func configureProcessGroup(cmd *exec.Cmd, owned bool) error {
	if owned {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	return nil
}
func stopProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Observe exit without reaping. The zombie pins the leader's PID/group number
// until command children have been signalled; only then may exec.Cmd.Wait reap.
func retireProcessGroup(pid int, stop func() error) error {
	var info [16]uint64 // Linux siginfo_t: 128 bytes, naturally aligned.
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return stop()
	}
}
