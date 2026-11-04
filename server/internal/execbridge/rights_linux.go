//go:build linux

package execbridge

import (
	"context"
	"net"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func expectedFiles(w BundleExpectation) []FileIdentity {
	if w.HasLease {
		return []FileIdentity{w.Config, w.Lease, w.Spool}
	}
	return []FileIdentity{w.Config, w.Spool}
}
func validateBundle(files []*os.File, w BundleExpectation) error {
	expected := expectedFiles(w)
	if len(files) != len(expected) {
		return ErrBundle
	}
	seen := map[FileIdentity]bool{}
	for i, f := range files {
		if f == nil {
			return ErrBundle
		}
		var stat unix.Stat_t
		if unix.Fstat(int(f.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
			return ErrBundle
		}
		identity := FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
		if identity.Inode == 0 || identity != expected[i] || seen[identity] {
			return ErrBundle
		}
		seen[identity] = true
		if i == 0 {
			const seals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
			actual, err := unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0)
			if err != nil || actual&seals != seals || stat.Size <= 0 || stat.Size > 65536 {
				return ErrBundle
			}
		}
		if i == len(files)-1 {
			flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
			if err != nil || flags&unix.O_ACCMODE != unix.O_RDWR || flags&unix.O_APPEND != 0 {
				return ErrBundle
			}
		}
	}
	return nil
}
func (c *HostChannel) SendBundle(ctx context.Context, files []*os.File, w BundleExpectation) error {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	return c.sendBundleLocked(ctx, files, w)
}
func (c *HostChannel) sendBundleLocked(ctx context.Context, files []*os.File, w BundleExpectation) error {
	u, ok := c.conn.(*net.UnixConn)
	if !ok || c.peer == nil || c.identity.Validate(true) != nil {
		return ErrBundle
	}
	if err := validateBundle(files, w); err != nil {
		return err
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
	n, sent, err := u.WriteMsgUnix([]byte{0x51}, oob, nil)
	runtime.KeepAlive(files)
	if err != nil || n != 1 || sent != len(oob) {
		return ErrBundle
	}
	return nil
}

func (c *BridgeChannel) ReceiveBundle(ctx context.Context, w BundleExpectation) (*Bundle, error) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	return c.receiveBundleLocked(ctx, w)
}
func (c *BridgeChannel) receiveBundleLocked(ctx context.Context, w BundleExpectation) (*Bundle, error) {
	u, ok := c.conn.(*net.UnixConn)
	if !ok || c.closed.Load() || !validBootstrap(c.bootstrap) {
		return nil, ErrBundle
	}
	files, err := receiveRightFiles(ctx, u, 0x51, 3)
	if err != nil {
		return nil, err
	}
	if validateBundle(files, w) != nil {
		for _, f := range files {
			f.Close()
		}
		return nil, ErrBundle
	}
	return &Bundle{Files: files}, nil
}
func receiveRightFiles(ctx context.Context, u *net.UnixConn, tag byte, maximum int) (out []*os.File, err error) {
	clear, err := authDeadline(ctx, u)
	if err != nil {
		return nil, ErrBundle
	}
	defer clear()
	raw, err := u.SyscallConn()
	if err != nil {
		return nil, ErrBundle
	}
	data := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(maximum*4))
	var n, oobn, flags int
	var receiveErr error
	err = raw.Read(func(fd uintptr) bool {
		n, oobn, flags, _, receiveErr = unix.Recvmsg(int(fd), data, oob, unix.MSG_CMSG_CLOEXEC)
		return receiveErr != unix.EAGAIN && receiveErr != unix.EWOULDBLOCK
	})
	if err != nil || receiveErr != nil {
		return nil, ErrBundle
	}
	// Always close descriptors already installed by the kernel, including on
	// control truncation or a malformed role/count/marker.
	var files []*os.File
	defer func() {
		if out == nil {
			for _, f := range files {
				f.Close()
			}
		}
	}()
	messages, parseErr := unix.ParseSocketControlMessage(oob[:oobn])
	bad := parseErr != nil
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			bad = true
			continue
		}
		fds, e := unix.ParseUnixRights(&message)
		if e != nil {
			bad = true
			continue
		}
		for _, fd := range fds {
			files = append(files, os.NewFile(uintptr(fd), "bridge-descriptor"))
		}
	}
	if bad || n != 1 || data[0] != tag || flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 || len(files) == 0 || len(files) > maximum {
		return nil, ErrBundle
	}
	for _, f := range files {
		fdFlags, e := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
		if e != nil || fdFlags&unix.FD_CLOEXEC == 0 {
			return nil, ErrBundle
		}
	}
	return files, nil
}
