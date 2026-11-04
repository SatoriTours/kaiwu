//go:build linux

package executionenv

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// NewSystemdInspector accepts a trusted administrator-selected absolute tool
// path, never a task's PATH/configuration. Construction does not call systemd.
func NewSystemdInspector(systemctl string) (*SystemdInspector, error) {
	if !absolutePath(systemctl) || systemctl == "/" {
		return nil, ErrInvalid
	}
	if os.Getuid() != os.Geteuid() {
		return nil, ErrUnavailable
	}
	runtimeDir := "/run/user/" + strconv.Itoa(os.Getuid())
	return &SystemdInspector{pin: pinNativeInspection, show: func(parent context.Context, unit string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, systemctl, "--user", "--no-pager", "--no-ask-password", "--plain", "show", "--property=Id,LoadState,ActiveState,SubState,InvocationID,ControlGroup,MainPID", "--", unit)
		cmd.Dir = "/"
		cmd.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + runtimeDir, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDir + "/bus", "SYSTEMD_COLORS=0"}
		cmd.WaitDelay = 250 * time.Millisecond
		output := &inspectionOutput{}
		cmd.Stdout = output
		cmd.Stderr = io.Discard
		if cmd.Run() != nil || ctx.Err() != nil || output.overflow {
			return nil, ErrUnavailable
		}
		return output.buffer.Bytes(), nil
	}}, nil
}

type inspectionOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *inspectionOutput) Write(p []byte) (int, error) {
	if len(p) > 65536-w.buffer.Len() {
		w.overflow = true
		return 0, ErrUnavailable
	}
	return w.buffer.Write(p)
}

func openInspectionDirectory(ctx context.Context, absolute string) (int, error) {
	if !absolutePath(absolute) {
		return -1, ErrUnavailable
	}
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, ErrUnavailable
	}
	defer unix.Close(root)
	if absolute == "/" {
		return walkInspectionDirectory(ctx, root, "")
	}
	return walkInspectionDirectory(ctx, root, strings.TrimPrefix(absolute, "/"))
}
func walkInspectionDirectory(ctx context.Context, root int, relative string) (int, error) {
	fd, err := unix.FcntlInt(uintptr(root), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, ErrUnavailable
	}
	if relative == "" {
		return fd, nil
	}
	for _, part := range strings.Split(relative, "/") {
		if ctx.Err() != nil || part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return -1, ErrUnavailable
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		unix.Close(fd)
		if e != nil {
			return -1, ErrUnavailable
		}
		fd = next
	}
	return fd, nil
}
func readInspectionFile(ctx context.Context, dir int, name string, limit int) (string, error) {
	if ctx.Err() != nil || name == "" || name == "." || name == ".." || strings.Contains(name, "/") || limit < 1 {
		return "", ErrUnavailable
	}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "kernel-inspection")
	defer f.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit || ctx.Err() != nil {
		return "", ErrUnavailable
	}
	return string(data), nil
}
func verifiedInspectionFS(fd int, expected int64) error {
	var stat unix.Statfs_t
	if unix.Fstatfs(fd, &stat) != nil || int64(stat.Type) != expected {
		return ErrUnavailable
	}
	return nil
}

type nativeInspection struct {
	proc, cgroups, group, process, pidfd int
	groupPath, pidPath                   string
}

func (p *nativeInspection) Close() error {
	for _, fd := range []*int{&p.pidfd, &p.process, &p.group, &p.cgroups, &p.proc} {
		if *fd >= 0 {
			unix.Close(*fd)
			*fd = -1
		}
	}
	return nil
}
func pinNativeInspection(ctx context.Context, cg string, pid int) (out kernelInspection, err error) {
	if !absolutePath(cg) || cg == "/" || pid < 1 || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	p := &nativeInspection{proc: -1, cgroups: -1, group: -1, process: -1, pidfd: -1, groupPath: strings.TrimPrefix(cg, "/"), pidPath: strconv.Itoa(pid)}
	defer func() {
		if out == nil {
			p.Close()
		}
	}()
	p.proc, err = openInspectionDirectory(ctx, "/proc")
	if err != nil || verifiedInspectionFS(p.proc, unix.PROC_SUPER_MAGIC) != nil {
		return nil, ErrUnavailable
	}
	p.cgroups, err = openInspectionDirectory(ctx, "/sys/fs/cgroup")
	if err != nil || verifiedInspectionFS(p.cgroups, unix.CGROUP2_SUPER_MAGIC) != nil {
		return nil, ErrUnavailable
	}
	p.group, err = walkInspectionDirectory(ctx, p.cgroups, p.groupPath)
	if err != nil || verifiedInspectionFS(p.group, unix.CGROUP2_SUPER_MAGIC) != nil {
		return nil, ErrUnavailable
	}
	// This pins the manager-selected MainPID for inspection. Socket peer identity
	// still exclusively uses SO_PEERPIDFD in execbridge, never this numeric open.
	p.pidfd, err = unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	p.process, err = walkInspectionDirectory(ctx, p.proc, p.pidPath)
	if err != nil {
		return nil, ErrUnavailable
	}
	return p, nil
}
func sameInspectionDirectory(ctx context.Context, root int, path string, pinned int) bool {
	fresh, err := walkInspectionDirectory(ctx, root, path)
	if err != nil {
		return false
	}
	defer unix.Close(fresh)
	var a, b unix.Stat_t
	return unix.Fstat(fresh, &a) == nil && unix.Fstat(pinned, &b) == nil && a.Dev == b.Dev && a.Ino == b.Ino
}
func (p *nativeInspection) Read(ctx context.Context) (kernelSample, error) {
	fail := func() (kernelSample, error) { return kernelSample{}, ErrUnavailable }
	if ctx.Err() != nil || p.pidfd < 0 || unix.PidfdSendSignal(p.pidfd, 0, nil, 0) != nil || !sameInspectionDirectory(ctx, p.cgroups, p.groupPath, p.group) || !sameInspectionDirectory(ctx, p.proc, p.pidPath, p.process) {
		return fail()
	}
	bootDir, err := walkInspectionDirectory(ctx, p.proc, "sys/kernel/random")
	if err != nil {
		return fail()
	}
	boot, err := readInspectionFile(ctx, bootDir, "boot_id", 128)
	unix.Close(bootDir)
	if err != nil {
		return fail()
	}
	membership, err := readInspectionFile(ctx, p.process, "cgroup", 8192)
	if err != nil || membership != "0::/"+p.groupPath+"\n" {
		return fail()
	}
	fields := map[string]string{}
	for _, name := range []string{"cpu.max", "memory.max", "memory.swap.max", "pids.max", "cgroup.events"} {
		value, e := readInspectionFile(ctx, p.group, name, 1024)
		if e != nil {
			return fail()
		}
		fields[name] = value
	}
	budget, populated, err := parseKernelLimits(fields)
	if err != nil {
		return fail()
	}
	if ctx.Err() != nil || unix.PidfdSendSignal(p.pidfd, 0, nil, 0) != nil || !sameInspectionDirectory(ctx, p.cgroups, p.groupPath, p.group) || !sameInspectionDirectory(ctx, p.proc, p.pidPath, p.process) {
		return fail()
	}
	return kernelSample{BootID: strings.TrimSuffix(boot, "\n"), ProcessGroup: "/" + p.groupPath, Populated: populated, Budget: budget}, nil
}
func parseKernelLimits(fields map[string]string) (Budget, bool, error) {
	fail := func() (Budget, bool, error) { return Budget{}, false, ErrUnavailable }
	if len(fields) != 5 {
		return fail()
	}
	cpu := strings.Fields(fields["cpu.max"])
	if len(cpu) != 2 || cpu[1] != "100000" {
		return fail()
	}
	quota, err := kernelNumber(cpu[0])
	if err != nil {
		return fail()
	}
	numbers := map[string]int64{}
	for _, name := range []string{"memory.max", "memory.swap.max", "pids.max"} {
		n, e := kernelNumber(strings.TrimSuffix(fields[name], "\n"))
		if e != nil {
			return fail()
		}
		numbers[name] = n
	}
	if numbers["memory.swap.max"] != 0 {
		return fail()
	}
	events := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(fields["cgroup.events"], "\n"), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return fail()
		}
		if _, seen := events[parts[0]]; seen {
			return fail()
		}
		events[parts[0]] = parts[1]
	}
	if len(events) != 2 || events["populated"] != "1" || events["frozen"] != "0" {
		return fail()
	}
	b := Budget{CPUQuotaUSec: quota, MemoryBytes: numbers["memory.max"], PidsMax: numbers["pids.max"]}
	if !validKernelBudget(b) {
		return fail()
	}
	return b, true, nil
}
