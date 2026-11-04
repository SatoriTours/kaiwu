//go:build linux

package execbridge

import (
	"context"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

// HardenBridge is irreversible and only for the dedicated bridge, before any
// config/private FDs. NNP MUST already be inherited across exec from a trusted
// launcher (the unit template uses NoNewPrivileges=yes). Setting PR_SET_NNP here
// would affect one Go OS thread only. Never call this in the host server.
// This is not namespace/mount/quota attestation or an environment memory wipe.
func HardenBridge() error {
	defer os.Clearenv()
	if inheritedBridgeNNP() != nil {
		return executionenv.ErrUnavailable
	}
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return executionenv.ErrUnavailable
	}
	return inheritedBridgeNNP()
}
func inheritedBridgeNNP() error {
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || nnp != 1 {
		return executionenv.ErrUnavailable
	}
	process, err := openMaterialPath(context.Background(), "/proc/"+strconv.Itoa(os.Getpid()))
	if err != nil {
		return executionenv.ErrUnavailable
	}
	defer process.Close()
	var filesystem unix.Statfs_t
	if unix.Fstatfs(int(process.Fd()), &filesystem) != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
		return executionenv.ErrUnavailable
	}
	// A stable, bounded census rejects current-thread-only NNP. We never patch
	// other threads. Once all existing threads inherit 1, descendants inherit 1
	// and Linux cannot clear it. Churn/absent proc evidence fails closed.
	for attempt := 0; attempt < 2; attempt++ {
		before, err := bridgeThreadCensus(process)
		if err != nil {
			return executionenv.ErrUnavailable
		}
		for _, tid := range before {
			if bridgeThreadNNP(process, tid) != nil {
				return executionenv.ErrUnavailable
			}
		}
		after, err := bridgeThreadCensus(process)
		if err != nil {
			return executionenv.ErrUnavailable
		}
		if strings.Join(before, ",") == strings.Join(after, ",") {
			return nil
		}
	}
	return executionenv.ErrUnavailable
}
func bridgeThreadCensus(process *os.File) ([]string, error) {
	task, err := openMaterialDirectory(process, "task")
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	defer task.Close()
	entries, err := task.ReadDir(4097)
	if err != nil && err != io.EOF {
		return nil, executionenv.ErrUnavailable
	}
	if len(entries) == 0 || len(entries) > 4096 {
		return nil, executionenv.ErrUnavailable
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		number, e := strconv.ParseUint(entry.Name(), 10, 32)
		if e != nil || number == 0 || strconv.FormatUint(number, 10) != entry.Name() {
			return nil, executionenv.ErrUnavailable
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids, nil
}
func bridgeThreadNNP(process *os.File, tid string) error {
	task, err := openMaterialDirectory(process, "task")
	if err != nil {
		return executionenv.ErrUnavailable
	}
	defer task.Close()
	thread, err := openMaterialDirectory(task, tid)
	if err != nil {
		return executionenv.ErrUnavailable
	}
	defer thread.Close()
	fd, err := unix.Openat(int(thread.Fd()), "status", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return executionenv.ErrUnavailable
	}
	status := os.NewFile(uintptr(fd), "bridge-thread-status")
	defer status.Close()
	var fs unix.Statfs_t
	var stat unix.Stat_t
	if unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.PROC_SUPER_MAGIC || unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return executionenv.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(status, 65537))
	if err != nil || len(data) > 65536 {
		return executionenv.ErrUnavailable
	}
	matches := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "NoNewPrivs:" {
			if len(fields) != 2 || fields[1] != "1" {
				return executionenv.ErrUnavailable
			}
			matches++
		}
	}
	if matches != 1 {
		return executionenv.ErrUnavailable
	}
	return nil
}
