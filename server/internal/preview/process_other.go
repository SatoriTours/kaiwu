//go:build !linux

package preview

import (
	"errors"
	"os"
	"os/exec"
)

func setupProcess(*exec.Cmd) {}

func openManifest(name string) (*os.File, error) { return os.Open(name) }

type processGroup struct{ cmd *exec.Cmd }

func (*processGroup) stop()         {}
func (p *processGroup) wait() error { return p.cmd.Wait() }
func groupGone(int) bool            { return false }
func ownedPort(int, int) error      { return errors.New("当前平台尚不支持受控任务预览") }
func acquireLock(string) (Lease, error) {
	return nil, errors.New("当前平台尚不支持受控任务预览")
}
