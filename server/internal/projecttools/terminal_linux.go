//go:build linux

package projecttools

import (
	"errors"
	"fmt"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func startPTY(dir string, lease preview.Lease) (*exec.Cmd, *os.File, error) {
	master, e := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, nil, e
	}
	fail := func(e error) (*exec.Cmd, *os.File, error) { master.Close(); return nil, nil, e }
	if e = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); e != nil {
		return fail(e)
	}
	number, e := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if e != nil {
		return fail(e)
	}
	slave, e := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if e != nil {
		return fail(e)
	}
	defer slave.Close()
	_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 100})
	shell, e := exec.LookPath("bash")
	if e != nil {
		return fail(errors.New("服务器未找到 bash"))
	}
	cmd := exec.Command(shell, "--noprofile", "--norc", "+m", "-i")
	cmd.Dir = dir
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	cmd.Env = []string{"TERM=xterm-256color", "PS1=工作区 $ ", "HISTFILE=/dev/null"}
	for _, key := range []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	if lease != nil {
		cmd.ExtraFiles = []*os.File{lease.File()}
		if e = lease.Attempt(); e != nil {
			return fail(e)
		}
	}
	if e = cmd.Start(); e != nil {
		return fail(e)
	}
	if lease != nil {
		if e = lease.Started(cmd.Process.Pid); e != nil {
			_ = signalGroup(cmd.Process.Pid)
			_ = cmd.Wait()
			return fail(e)
		}
	}
	if _, e = master.WriteString("set +m\n"); e != nil {
		_ = signalGroup(cmd.Process.Pid)
		_ = cmd.Wait()
		return fail(e)
	}
	return cmd, master, nil
}
func resizePTY(f *os.File, cols, rows int) error {
	return unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)})
}
func signalGroup(pid int) error { return syscall.Kill(-pid, syscall.SIGKILL) }
func stopGroup(pid int) bool {
	_ = signalGroup(pid)
	for i := 0; i < 40; i++ {
		if syscall.Kill(-pid, 0) == syscall.ESRCH {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}
