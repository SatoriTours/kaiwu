//go:build linux

package preview

import (
	"os"

	"golang.org/x/sys/unix"
)

// openStaticAt walks root and then the asset path one segment at a time from
// the worktree directory, so a check and its use share the same opened file:
//   - every directory segment is openat(O_NOFOLLOW|O_DIRECTORY|O_CLOEXEC),
//     which refuses symlinks, FIFOs, devices and stale components;
//   - the leaf is opened O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC and then fstat'ed, so
//     a FIFO or device can never block the open and a swapped file is caught by
//     the caller's regular-file/size checks on the same descriptor.
//
// All intermediate descriptors are closed on every path.
func openStaticAt(tree string, dirs []string, leaf string) (*os.File, os.FileInfo, error) {
	dir, e := unix.Open(tree, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, nil, e
	}
	walk := func(name string) bool {
		next, e := unix.Openat(dir, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(dir)
		if e != nil {
			dir = -1
			return false
		}
		dir = next
		return true
	}
	for _, name := range dirs {
		if !walk(name) {
			return nil, nil, unix.EACCES
		}
	}
	fd, e := unix.Openat(dir, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	unix.Close(dir)
	if e != nil {
		return nil, nil, e
	}
	f := os.NewFile(uintptr(fd), leaf)
	if f == nil {
		unix.Close(fd)
		return nil, nil, unix.EBADF
	}
	info, e := f.Stat()
	if e != nil {
		return f, nil, e
	}
	return f, info, nil
}
