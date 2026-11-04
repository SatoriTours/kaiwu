//go:build unix

package config

import (
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// openExecutionRoot pins every component atomically without following links.
// Starting from '/' avoids both ancestor redirection and a mutable cwd. Missing
// components are returned as ENOENT without creating directories.
func openExecutionRoot(name string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	fd, err := unix.Open("/", flags, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), name), nil
}
func openExecutionFile(root *os.File) (*os.File, error) {
	// Nonblocking open closes the regular-file-check/FIFO-swap hang. O_NOFOLLOW
	// closes final link replacement. The caller fstats the pinned descriptor
	// before consuming bytes, rather than re-opening a checked pathname.
	fd, err := unix.Openat(int(root.Fd()), "sandbox.json", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	runtime.KeepAlive(root)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "sandbox.json"), nil
}
