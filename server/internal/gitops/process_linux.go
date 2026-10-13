//go:build linux

package gitops

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	stopped := false
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return os.ErrProcessDone
		}
		stopped = true
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Cancel()
	}
}

func waitProcess(cmd *exec.Cmd) error {
	// WNOWAIT leaves the exited leader unreaped, reserving its PID/PGID while
	// descendants are stopped. Never signal a numeric group after reaping it.
	var info unix.Siginfo
	var e error
	for {
		e = unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if e != syscall.EINTR {
			break
		}
	}
	if e == nil {
		killProcess(cmd)
	}
	return errors.Join(e, cmd.Wait())
}
