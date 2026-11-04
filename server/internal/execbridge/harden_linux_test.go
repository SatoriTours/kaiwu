//go:build linux

package execbridge

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

func TestBridgeHardeningHelper(t *testing.T) {
	if os.Getenv("KAIWU_HARDEN_FIXTURE") != "1" {
		return
	}
	mode := os.Getenv("KAIWU_HARDEN_MODE")
	if mode == "preexec" {
		// Replace this isolated launcher, not a naked fork or a patch of Go runtime
		// threads. Every thread of the new Go image inherits NNP=1 from execve.
		runtime.LockOSThread()
		if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
			os.Exit(20)
		}
		exe, err := os.Executable()
		if err != nil {
			os.Exit(20)
		}
		unix.Exec(exe, []string{exe, "-test.run=^TestBridgeHardeningHelper$"}, []string{"KAIWU_HARDEN_FIXTURE=1", "KAIWU_HARDEN_MODE=check", "KAIWU_SYNTHETIC_SECRET=not-a-credential"})
		os.Exit(20)
	}
	if mode == "plain" || mode == "mixed" {
		runtime.LockOSThread()
		initial, _ := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
		if initial != 0 {
			os.Exit(77)
		}
		if mode == "mixed" {
			ready := make(chan struct{})
			block := make(chan struct{})
			go func() { runtime.LockOSThread(); close(ready); <-block }()
			<-ready
			if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
				os.Exit(21)
			}
		}
		if !errors.Is(HardenBridge(), executionenv.ErrUnavailable) {
			os.Exit(41)
		}
		os.Exit(0)
	}
	if mode != "check" || HardenBridge() != nil {
		os.Exit(21)
	}
	var limit unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
		os.Exit(22)
	}
	dump, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil || dump != 0 {
		os.Exit(23)
	}
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || nnp != 1 || len(os.Environ()) != 0 {
		os.Exit(24)
	}
	runtime.LockOSThread()
	observed := make(chan int, 1)
	go func() {
		runtime.LockOSThread()
		nnp, e := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
		if e != nil {
			nnp = -1
		}
		observed <- nnp
		runtime.UnlockOSThread()
	}()
	if <-observed != 1 {
		os.Exit(25)
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(26)
	}
	cmd := exec.Command(exe, "-test.run=^TestBridgeExecInheritanceHelper$")
	cmd.Env = []string{"KAIWU_HARDEN_EXEC_CHILD=1"}
	if cmd.Run() != nil {
		os.Exit(27)
	}
	os.Exit(0)
}
func TestBridgeExecInheritanceHelper(t *testing.T) {
	if os.Getenv("KAIWU_HARDEN_EXEC_CHILD") != "1" {
		return
	}
	current, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || current != 1 {
		os.Exit(28)
	}
	runtime.LockOSThread()
	result := make(chan int, 1)
	go func() {
		runtime.LockOSThread()
		n, _ := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
		result <- n
		runtime.UnlockOSThread()
	}()
	if <-result != 1 {
		os.Exit(29)
	}
	os.Exit(0)
}
func runHardeningHelper(t *testing.T, mode string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBridgeHardeningHelper$")
	cmd.Env = []string{"KAIWU_HARDEN_FIXTURE=1", "KAIWU_HARDEN_MODE=" + mode}
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 77 {
		t.Skip("inherited NNP=1 cannot be cleared to test missing precondition")
	}
	if err != nil {
		t.Fatalf("hardening %s: %v %s", mode, err, output)
	}
}
func TestHardeningIsRealAndOnlyInPrivateHelper(t *testing.T) { runHardeningHelper(t, "preexec") }
func TestHardeningRefusesMissingInheritedNNP(t *testing.T)   { runHardeningHelper(t, "plain") }
func TestHardeningRefusesCallingThreadOnlyNNP(t *testing.T)  { runHardeningHelper(t, "mixed") }
