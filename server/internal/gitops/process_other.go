//go:build !linux

package gitops

import "os/exec"

func setupProcess(cmd *exec.Cmd) {}
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func waitProcess(cmd *exec.Cmd) error { return cmd.Wait() }
