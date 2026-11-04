//go:build linux

package executionenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestKernelBudgetParsingRejectsSoftAndUnboundedLimits(t *testing.T) {
	valid := map[string]string{"cpu.max": "25000 100000\n", "memory.max": "100663296\n", "memory.swap.max": "0\n", "pids.max": "32\n", "cgroup.events": "populated 1\nfrozen 0\n"}
	b, pop, err := parseKernelLimits(valid)
	if err != nil || !pop || b.CPUQuotaUSec != 25000 || b.MemoryBytes != 100663296 || b.PidsMax != 32 || b.DiskBytes != 0 {
		t.Fatal("lost actual limits or invented quota", err)
	}
	for _, bad := range []struct{ k, v string }{{"cpu.max", "max 100000\n"}, {"cpu.max", "25000 50000\n"}, {"cpu.max", "0 100000\n"}, {"memory.max", "max\n"}, {"memory.max", "-1\n"}, {"memory.swap.max", "1\n"}, {"pids.max", "max\n"}, {"cgroup.events", "populated 0\nfrozen 0\n"}, {"cgroup.events", "populated 1\nfrozen 1\n"}, {"cgroup.events", "populated 1\npopulated 0\nfrozen 0\n"}} {
		input := map[string]string{}
		for k, v := range valid {
			input[k] = v
		}
		input[bad.k] = bad.v
		if _, _, err := parseKernelLimits(input); err == nil {
			t.Fatalf("accepted %s=%q", bad.k, bad.v)
		}
	}
}
func TestPinnedKernelFilesRejectSymlinksFIFOAndRebinding(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "group"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "group", "value"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := openInspectionDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	group, err := walkInspectionDirectory(context.Background(), fd, "group")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(group)
	if err = os.Rename(filepath.Join(root, "group"), filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("old", filepath.Join(root, "group")); err != nil {
		t.Fatal(err)
	}
	if _, err = walkInspectionDirectory(context.Background(), fd, "group"); err == nil {
		t.Fatal("followed rebound ancestor")
	}
	data, err := readInspectionFile(context.Background(), group, "value", 16)
	if err != nil || data != "old\n" {
		t.Fatal("lost pinned object")
	}
	if err = os.Symlink("value", filepath.Join(root, "old", "link")); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mkfifo(filepath.Join(root, "old", "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link", "fifo", "../old/value"} {
		start := time.Now()
		if _, err = readInspectionFile(context.Background(), group, name, 16); err == nil || time.Since(start) > time.Second {
			t.Fatal("unsafe or blocking special file")
		}
	}
	if err = os.WriteFile(filepath.Join(root, "old", "large"), []byte(strings.Repeat("x", 17)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readInspectionFile(context.Background(), group, "large", 16); err == nil {
		t.Fatal("unbounded read")
	}
}
func TestSystemctlInspectionHelper(t *testing.T) {
	hasUser := false
	for _, a := range os.Args {
		hasUser = hasUser || a == "--user"
	}
	if !hasUser {
		return
	}
	// These are direct synthetic helpers only. No systemctl, bus or units run.
	if os.Getenv("HOME") != "" || os.Getenv("KAIWU_INSPECT_SECRET") != "" || os.Getenv("LC_ALL") != "C" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "unix:path=/run/user/"+fmt.Sprint(os.Getuid())+"/bus" {
		os.Exit(32)
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(33)
	}
	args = args[1:]
	expected := []string{"--user", "--no-pager", "--no-ask-password", "--plain", "show", "--property=Id,LoadState,ActiveState,SubState,InvocationID,ControlGroup,MainPID", "--", inspectIdentity().Unit}
	if strings.Join(args, "\n") != strings.Join(expected, "\n") {
		os.Exit(34)
	}
	// Script chooses the mode via the test executable's own extra test selector.
	for _, arg := range os.Args {
		if arg == "-test.v=true" {
			fmt.Print(strings.Repeat("x", 70000))
			os.Exit(0)
		}
	}
	fmt.Print(inspectShow(inspectIdentity()))
	os.Exit(0)
}
func TestNativeShowUsesOnlyBoundedReadOnlyCommandAndCleanEnvironment(t *testing.T) {
	t.Setenv("KAIWU_INSPECT_SECRET", "synthetic-not-a-credential")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, oversize := range []bool{false, true} {
		script := filepath.Join(t.TempDir(), "synthetic-systemctl")
		flag := ""
		if oversize {
			flag = " -test.v=true"
		}
		body := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=^TestSystemctlInspectionHelper$" + flag + " -- \"$@\"\n"
		if err = os.WriteFile(script, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		inspector, err := NewSystemdInspector(script)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := inspector.show(context.Background(), inspectIdentity().Unit)
		if oversize {
			if err == nil {
				t.Fatal("accepted oversized tool output")
			}
		} else if err != nil || string(raw) != inspectShow(inspectIdentity()) {
			t.Fatal("unsafe command or environment", err)
		}
	}
	if _, err = NewSystemdInspector("systemctl"); err == nil {
		t.Fatal("accepted ambient PATH tool")
	}
}
func TestNativeInspectionRejectsSyntheticFilesystemAsKernelEvidence(t *testing.T) {
	fd, err := openInspectionDirectory(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if verifiedInspectionFS(fd, unix.CGROUP2_SUPER_MAGIC) == nil || verifiedInspectionFS(fd, unix.PROC_SUPER_MAGIC) == nil {
		t.Fatal("ordinary temp files became kernel evidence")
	}
}

// Reads only this local process and kernel anchors. No unit, fake filesystem or
// synthetic snapshot is used as a positive kernel filesystem/PIDFD assertion.
func TestNativeInspectionPinsRealLocalProcAndPIDFD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	proc, err := openInspectionDirectory(ctx, "/proc")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(proc)
	if verifiedInspectionFS(proc, unix.PROC_SUPER_MAGIC) != nil {
		t.Fatal("proc anchor is not procfs")
	}
	pidDir, err := walkInspectionDirectory(ctx, proc, strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidDir)
	membership, err := readInspectionFile(ctx, pidDir, "cgroup", 8192)
	if err != nil || !strings.Contains(membership, "0::") {
		t.Fatal("local cgroup v2 membership unreadable")
	}
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Skip("kernel lacks inspection pidfd")
	}
	defer unix.Close(pidfd)
	if unix.PidfdSendSignal(pidfd, 0, nil, 0) != nil {
		t.Fatal("own pinned process not live")
	}
	if !sameInspectionDirectory(ctx, proc, strconv.Itoa(os.Getpid()), pidDir) {
		t.Fatal("lost pinned proc identity")
	}
}
