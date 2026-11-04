//go:build linux

package execbridge

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func unixPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	convert := func(fd int) *net.UnixConn {
		f := os.NewFile(uintptr(fd), "socketpair")
		c, err := net.FileConn(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		u := c.(*net.UnixConn)
		t.Cleanup(func() { u.Close() })
		return u
	}
	return convert(fds[0]), convert(fds[1])
}
func authenticatedPair(t *testing.T) (*HostChannel, *BridgeChannel) {
	t.Helper()
	b, key, id := authFixture(t)
	l, r := unixPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	type result struct {
		c   *BridgeChannel
		err error
	}
	done := make(chan result, 1)
	go func() { c, err := AuthenticateBridge(ctx, r, b); done <- result{c, err} }()
	host, err := AuthenticateHost(ctx, l, b, key, fixedVerifier{&fixedPeer{identity: id}})
	if err != nil {
		t.Fatal(err)
	}
	other := <-done
	if other.err != nil {
		t.Fatal(other.err)
	}
	t.Cleanup(func() { host.Detach(); other.c.Detach() })
	return host, other.c
}
func descriptorFixture(t *testing.T) ([]*os.File, BundleExpectation) {
	t.Helper()
	fd, err := unix.MemfdCreate("kaiwu-test-config", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	cfg := os.NewFile(uintptr(fd), "config")
	t.Cleanup(func() { cfg.Close() })
	if _, err := cfg.WriteString("synthetic configuration"); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(cfg.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		t.Fatal(err)
	}
	lease, err := os.CreateTemp(t.TempDir(), "lease")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Close() })
	if err := unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	spool, err := os.CreateTemp(t.TempDir(), "spool")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { spool.Close() })
	identity := func(f *os.File) FileIdentity {
		var stat unix.Stat_t
		if err := unix.Fstat(int(f.Fd()), &stat); err != nil {
			t.Fatal(err)
		}
		return FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
	}
	return []*os.File{cfg, lease, spool}, BundleExpectation{Config: identity(cfg), Lease: identity(lease), Spool: identity(spool), HasLease: true}
}
func TestSCMRightsPreservesSameLeaseOFDAfterDetach(t *testing.T) {
	host, bridge := authenticatedPair(t)
	files, want := descriptorFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- host.SendBundle(ctx, files, want) }()
	received, err := bridge.ReceiveBundle(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	defer received.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, f := range received.Files {
		flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("received descriptor not CLOEXEC")
		}
	}
	name := files[1].Name()
	files[1].Close()
	host.Detach()
	bridge.Detach()
	other, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("lease dropped with host copy/control EOF: %v", err)
	}
	received.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("lease not released after last OFD closed: %v", err)
	}
}
func TestBundleRejectsUnsealedConfigAndWrongInode(t *testing.T) {
	for _, name := range []string{"unsealed", "inode"} {
		t.Run(name, func(t *testing.T) {
			host, bridge := authenticatedPair(t)
			files, want := descriptorFixture(t)
			if name == "unsealed" {
				replacement, err := os.OpenFile(filepath.Join(t.TempDir(), "config"), os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer replacement.Close()
				files[0] = replacement
			} else {
				want.Spool.Inode++
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := host.SendBundle(ctx, files, want); err == nil {
				t.Fatal("invalid bundle sent")
			}
			// A failed sender must not allow its control channel to be mistaken for
			// PREPARED; no receive/launch is attempted and no lease ownership changes.
			_ = bridge
		})
	}
}

func TestReceiverRejectsMalformedRightsAndClosesReceivedLease(t *testing.T) {
	for _, count := range []int{1, 4} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			host, bridge := authenticatedPair(t)
			files, want := descriptorFixture(t)
			descriptors := []int{int(files[1].Fd())}
			if count == 4 {
				descriptors = []int{int(files[0].Fd()), int(files[1].Fd()), int(files[2].Fd()), int(files[1].Fd())}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, _, err := host.conn.(*net.UnixConn).WriteMsgUnix([]byte{0x51}, unix.UnixRights(descriptors...), nil)
				done <- err
			}()
			if b, err := bridge.ReceiveBundle(ctx, want); err == nil {
				b.Close()
				t.Fatal("wrong descriptor count accepted")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			name := files[1].Name()
			files[1].Close()
			other, err := os.OpenFile(name, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatalf("rejected ancillary frame leaked lease FD: %v", err)
			}
		})
	}
}
