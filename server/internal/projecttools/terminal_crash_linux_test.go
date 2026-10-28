//go:build linux

package projecttools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type crashTestLease struct{ file *os.File }

func (l crashTestLease) File() *os.File     { return l.file }
func (l crashTestLease) Attempt() error     { return nil }
func (l crashTestLease) Started(int) error  { return nil }
func (l crashTestLease) Release(bool) error { return l.file.Close() }

func TestTerminalHostCrashReleasesOrdinaryShellLease(t *testing.T) {
	if dir := os.Getenv("KAIWU_PTY_CRASH_CHILD"); dir != "" {
		f, e := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			panic(e)
		}
		if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
			panic(e)
		}
		m := NewTerminals(context.Background())
		s, e := m.Start("crash", dir, crashTestLease{f})
		if e != nil {
			panic(e)
		}
		if e = m.Input(s.ID, "sleep 30 & printf 'CRASH_READY\\n'\n"); e != nil {
			panic(e)
		}
		ready := false
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			v, _ := m.Read(s.ID)
			if strings.Contains(v.Output, "\r\nCRASH_READY\r\n") {
				ready = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ready {
			panic("shell command did not start before crash test")
		}
		if e = json.NewEncoder(os.Stdout).Encode(m.entries[s.ID].cmd.Process.Pid); e != nil {
			panic(e)
		}
		select {}
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTerminalHostCrashReleasesOrdinaryShellLease$")
	cmd.Env = append(os.Environ(), "KAIWU_PTY_CRASH_CHILD="+dir)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scan := bufio.NewScanner(pipe)
	if !scan.Scan() {
		t.Fatal("child metadata missing", scan.Err())
	}
	var group int
	if json.Unmarshal(scan.Bytes(), &group) != nil || group <= 0 {
		t.Fatal("bad child group")
	}
	defer syscall.Kill(-group, syscall.SIGKILL) // only this test's freshly-created process group
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	waited = true
	lock, e := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("ordinary bash/child retained inherited lease after host SIGKILL", e)
}
