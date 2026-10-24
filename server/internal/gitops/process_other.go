//go:build !linux

package gitops

import "os/exec"

func setupProcess(cmd *exec.Cmd) {}

// groupGone cannot be proven on this platform, so ownership is kept.
func groupGone(int) bool { return false }

func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func waitProcess(cmd *exec.Cmd) error { return cmd.Wait() }

// StartGroup/KillGroup/WaitGroup expose the bounded process-group lifecycle
// for sibling packages (delivery local checks).
func StartGroup(cmd *exec.Cmd) { setupProcess(cmd) }
func KillGroup(cmd *exec.Cmd)  { killProcess(cmd) }
func WaitGroup(cmd *exec.Cmd) error {
	return waitProcess(cmd)
}
