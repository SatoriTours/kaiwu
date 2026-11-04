//go:build linux

package execbridge

import (
	"context"
	"net"
	"os"
	"sync"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

type nativePeer struct {
	mu        sync.Mutex
	fd, pid   int
	expected  executionenv.Identity
	inspector UnitPeerInspector
}

func (v NativeVerifier) Pin(ctx context.Context, c net.Conn) (PinnedPeer, error) {
	if ctx.Err() != nil || v.Inspector == nil || v.Expected.Validate(true) != nil {
		return nil, ErrAuth
	}
	socket, ok := c.(*net.UnixConn)
	if !ok {
		return nil, ErrAuth
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return nil, ErrAuth
	}
	fd, pid := -1, 0
	var socketErr error
	err = raw.Control(func(socketFD uintptr) {
		credentials, e := unix.GetsockoptUcred(int(socketFD), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil || credentials.Pid <= 0 || credentials.Uid != uint32(os.Geteuid()) {
			socketErr = ErrAuth
			return
		}
		pid = int(credentials.Pid)
		fd, e = unix.GetsockoptInt(int(socketFD), unix.SOL_SOCKET, unix.SO_PEERPIDFD)
		if e != nil || fd < 0 {
			socketErr = ErrAuth
			return
		}
		flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if e != nil || flags&unix.FD_CLOEXEC == 0 {
			socketErr = ErrAuth
		}
	})
	if err != nil || socketErr != nil || fd < 0 {
		if fd >= 0 {
			unix.Close(fd)
		}
		return nil, ErrAuth
	}
	return &nativePeer{fd: fd, pid: pid, expected: v.Expected, inspector: v.Inspector}, nil
}
func (p *nativePeer) Inspect(ctx context.Context) (executionenv.Identity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Signal zero is only a liveness/permission query; this adapter never sends a
	// stopping/reconnect signal. Never replace this pin with PidfdOpen(peerPID),
	// which would reopen a reusable number instead of the socket's actual peer.
	if ctx.Err() != nil || p.fd < 0 || unix.PidfdSendSignal(p.fd, 0, nil, 0) != nil {
		return executionenv.Identity{}, ErrAuth
	}
	state, err := p.inspector.InspectBridge(ctx, p.expected)
	if err != nil || state.MainPID != p.pid || !executionenv.CanStop(p.expected, state.Observation) || unix.PidfdSendSignal(p.fd, 0, nil, 0) != nil {
		return executionenv.Identity{}, ErrAuth
	}
	return p.expected, nil
}
func (p *nativePeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fd >= 0 {
		err := unix.Close(p.fd)
		p.fd = -1
		return err
	}
	return nil
}
