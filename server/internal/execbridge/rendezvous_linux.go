//go:build linux

package execbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

type socketRegistration struct {
	SchemaVersion     int
	Directory, Socket FileIdentity
}

func (m *RunMaterial) Listen(ctx context.Context) (out *ControlListener, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || ctx.Err() != nil {
		return nil, executionenv.ErrUnavailable
	}
	lock, _, err := readMaterialFile(ctx, m.run, "control.lock", 1, 0600)
	if errors.Is(err, unix.ENOENT) {
		if writeMaterialFile(ctx, m.run, "control.lock", []byte{1}, 0600) != nil {
			return nil, executionenv.ErrUnavailable
		}
		lock, _, err = readMaterialFile(ctx, m.run, "control.lock", 1, 0600)
	}
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	defer func() {
		if out == nil {
			lock.Close()
		}
	}()
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return nil, executionenv.ErrConflict
	}

	duplicate, dupErr := unix.FcntlInt(m.public.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if dupErr != nil {
		return nil, executionenv.ErrUnavailable
	}
	directory := os.NewFile(uintptr(duplicate), "listener-directory")
	defer func() {
		if out == nil {
			directory.Close()
		}
	}()
	var pending unix.Stat_t
	if e := unix.Fstatat(int(m.run.Fd()), "socket.next", &pending, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(e, unix.ENOENT) {
		return nil, executionenv.ErrUnavailable
	}
	marker, data, markerErr := readMaterialFile(ctx, m.run, "socket.json", 1024, 0600)
	registered := markerErr == nil
	var saved socketRegistration
	if registered {
		marker.Close()
		if strictBootstrapJSON(data, &saved) != nil || saved.SchemaVersion != 1 || saved.Directory != m.directory || saved.Socket.Inode == 0 {
			return nil, executionenv.ErrUnavailable
		}
	} else if !errors.Is(markerErr, unix.ENOENT) {
		return nil, executionenv.ErrUnavailable
	}
	var stat unix.Stat_t
	e := unix.Fstatat(int(m.public.Fd()), "control.sock", &stat, unix.AT_SYMLINK_NOFOLLOW)
	if e == nil {
		if !registered || saved.Socket != (FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}) || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != uint32(os.Geteuid()) {
			return nil, executionenv.ErrUnavailable
		}
		// The public directory is never task-writable. This same-run host-control
		// flock serializes its sole host writer, so no competing host may replace the
		// checked entry. Never use connection refusal as authority to unlink a path.
		if unix.Unlinkat(int(m.public.Fd()), "control.sock", 0) != nil || m.public.Sync() != nil {
			return nil, executionenv.ErrUnavailable
		}
	} else if !errors.Is(e, unix.ENOENT) {
		return nil, executionenv.ErrUnavailable
	}
	address := "/proc/self/fd/" + strconv.FormatUint(uint64(directory.Fd()), 10) + "/control.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	listener.SetUnlinkOnClose(false)
	defer func() {
		if out == nil {
			listener.Close()
		}
	}()
	if unix.Fstatat(int(m.public.Fd()), "control.sock", &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return nil, executionenv.ErrUnavailable
	}
	registration := socketRegistration{SchemaVersion: 1, Directory: m.directory, Socket: FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}}
	data, _ = json.Marshal(registration)
	// A failed exclusive stage is evidence, not disposable material. If any prior
	// interrupted publication left it behind, refuse instead of cleaning it.
	if writeMaterialFile(ctx, m.run, "socket.next", data, 0600) != nil || unix.Renameat(int(m.run.Fd()), "socket.next", int(m.run.Fd()), "socket.json") != nil || m.run.Sync() != nil || m.public.Sync() != nil || ctx.Err() != nil {
		return nil, executionenv.ErrUnavailable
	}
	return &ControlListener{listener: listener, lock: lock, directory: directory}, nil
}
