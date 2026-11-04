//go:build linux

package execbridge

import (
	"context"
	"net"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func sendRetained(ctx context.Context, c *BridgeChannel, files []*os.File) error {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	u, ok := c.conn.(*net.UnixConn)
	if !ok || c.closed.Load() || len(files) < 1 || len(files) > 2 {
		return ErrBundle
	}
	clear, err := authDeadline(ctx, u)
	if err != nil {
		return ErrBundle
	}
	defer clear()
	fds := make([]int, len(files))
	for i, f := range files {
		fds[i] = int(f.Fd())
	}
	oob := unix.UnixRights(fds...)
	n, sent, err := u.WriteMsgUnix([]byte{0x52}, oob, nil)
	runtime.KeepAlive(files)
	if err != nil || n != 1 || sent != len(oob) {
		return ErrBundle
	}
	return nil
}
func receiveRetained(ctx context.Context, c *HostChannel, w BundleExpectation) (out *Bundle, err error) {
	u, ok := c.conn.(*net.UnixConn)
	if !ok || c.closed.Load() || c.peer == nil {
		return nil, ErrBundle
	}
	files, err := receiveRightFiles(ctx, u, 0x52, 2)
	if err != nil {
		return nil, err
	}
	defer func() {
		if out == nil {
			for _, f := range files {
				f.Close()
			}
		}
	}()
	expected := []FileIdentity{w.Spool}
	if w.HasLease {
		expected = []FileIdentity{w.Lease, w.Spool}
	}
	if len(files) != len(expected) {
		return nil, ErrBundle
	}
	seen := map[FileIdentity]bool{}
	for i, f := range files {
		var stat unix.Stat_t
		if unix.Fstat(int(f.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
			return nil, ErrBundle
		}
		id := FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
		if id.Inode == 0 || id != expected[i] || seen[id] {
			return nil, ErrBundle
		}
		seen[id] = true
		if i == len(files)-1 {
			flags, e := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
			if e != nil || flags&unix.O_ACCMODE != unix.O_RDWR || flags&unix.O_APPEND != 0 {
				return nil, ErrBundle
			}
		}
	}
	return &Bundle{Files: files}, nil
}
