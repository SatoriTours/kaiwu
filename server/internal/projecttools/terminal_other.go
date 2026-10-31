//go:build !linux

package projecttools

import (
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"os"
	"os/exec"
)

func startPTY(string, preview.Lease) (*exec.Cmd, *os.File, error) {
	return nil, nil, errors.New("当前终端仅支持 Linux")
}
func signalGroup(int) error              { return errors.New("当前终端仅支持 Linux") }
func resizePTY(*os.File, int, int) error { return errors.New("当前终端仅支持 Linux") }
func stopGroup(int) bool                 { return false }
