//go:build linux

package pirpc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessGroupHelper(t *testing.T) {
	if os.Getenv("KAIWU_GROUP_HELPER") != "1" {
		t.Skip("helper")
	}
	if os.Getenv("KAIWU_GROUP_LEAF") == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	exe, _ := os.Executable()
	child := exec.Command(exe, "-test.run=^TestProcessGroupHelper$")
	child.Env = append(os.Environ(), "KAIWU_GROUP_LEAF=1")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if child.Start() != nil {
		os.Exit(2)
	}
	_ = os.WriteFile(os.Getenv("KAIWU_GROUP_MARKER"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
	if os.Getenv("KAIWU_GROUP_EXIT") == "1" {
		os.Exit(0)
	}
	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
	os.Exit(0)
}

func TestOwnedProcessGroupStopsChildrenAfterLeaderExit(t *testing.T) {
	for _, exit := range []string{"0", "1"} {
		t.Run(exit, func(t *testing.T) {
			exe, _ := os.Executable()
			dir := t.TempDir()
			marker := filepath.Join(dir, "child.pid")
			spec := LaunchSpec{Program: exe, Args: []string{"-test.run=^TestProcessGroupHelper$"}, Dir: dir, Env: []string{"KAIWU_GROUP_HELPER=1", "KAIWU_GROUP_EXIT=" + exit, "KAIWU_GROUP_MARKER=" + marker, "GORACE=atexit_sleep_ms=0"}, OwnProcessGroup: true}
			p, e := startProcess(spec)
			if e != nil {
				t.Fatal(e)
			}
			defer p.closeReaders()
			defer p.stop(time.Second, time.Second)
			var pid int
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				b, _ := os.ReadFile(marker)
				pid, _ = strconv.Atoi(string(b))
				if pid > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("child not started")
			}
			t.Cleanup(func() {
				if child, e := os.FindProcess(pid); e == nil {
					_ = child.Kill()
				}
			})
			r, _, e := p.stop(time.Second, time.Second)
			if e != nil || !r.Reaped {
				t.Fatal(r, e)
			}
			for time.Now().Before(deadline) {
				b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
				if os.IsNotExist(e) || strings.HasPrefix(strings.SplitN(string(b), ") ", 2)[1], "Z ") {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("owned command child survived stop")
		})
	}
}

func TestGroupRetirementPinsLeaderUntilChildrenAreStopped(t *testing.T) {
	exe, _ := os.Executable()
	marker := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command(exe, "-test.run=^TestProcessGroupHelper$")
	cmd.Env = []string{"KAIWU_GROUP_HELPER=1", "KAIWU_GROUP_EXIT=1", "KAIWU_GROUP_MARKER=" + marker, "GORACE=atexit_sleep_ms=0"}
	if e := configureProcessGroup(cmd, true); e != nil {
		t.Fatal(e)
	}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Wait()
	called := false
	if e := retireProcessGroup(cmd.Process.Pid, func() error {
		called = true
		b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid))
		if e != nil || !strings.HasPrefix(strings.SplitN(string(b), ") ", 2)[1], "Z ") {
			t.Error("leader was reaped before group retirement")
		}
		return stopProcessGroup(cmd.Process.Pid)
	}); e != nil {
		t.Fatal(e)
	}
	if !called {
		t.Fatal("group was not retired")
	}
}
