//go:build linux

package execbridge

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRendezvousRebindKeepsDirectoryAndExcludesLiveHost(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	first, err := m.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	again, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if duplicate, err := again.Listen(context.Background()); err == nil {
		duplicate.Close()
		t.Fatal("two live host listeners")
	}
	first.Close()
	next, err := again.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if again.DirectoryIdentity() != m.DirectoryIdentity() {
		t.Fatal("rebind replaced mounted directory")
	}
	// Closing the old wrapper again cannot release the new host's control lease.
	first.Close()
	if duplicate, err := m.Listen(context.Background()); err == nil {
		duplicate.Close()
		t.Fatal("old Close released current lock")
	}
}
func TestRendezvousNeverUnlinksUnregisteredOrReplacedEntry(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	socket := filepath.Join(root, b.Identity.RunID, "public", "control.sock")
	if err = os.WriteFile(socket, []byte("unknown-preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if listener, err := m.Listen(context.Background()); err == nil {
		listener.Close()
		t.Fatal("adopted unknown socket entry")
	}
	raw, err := os.ReadFile(socket)
	if err != nil || string(raw) != "unknown-preserve" {
		t.Fatal("destroyed unknown entry")
	}
}
func TestRendezvousDoesNotOverwriteUnknownRegistration(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	marker := filepath.Join(root, b.Identity.RunID, "socket.json")
	if err = os.WriteFile(marker, []byte("unknown-preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if listener, err := m.Listen(context.Background()); err == nil {
		listener.Close()
		t.Fatal("overwrote unknown registration")
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "unknown-preserve" {
		t.Fatal("registration destroyed")
	}
}

func TestBootstrapHostDeathHelper(t *testing.T) {
	if os.Getenv("KAIWU_BOOTSTRAP_HOST_FIXTURE") != "1" {
		return
	}
	b, _, _ := authFixture(t)
	m, err := PrepareRunMaterial(context.Background(), os.Getenv("KAIWU_BOOTSTRAP_ROOT"), b.Identity, b.SpecHash)
	if err != nil {
		os.Exit(31)
	}
	if _, err = m.Listen(context.Background()); err != nil {
		os.Exit(32)
	}
	// No Close/defer: the kernel, not a cooperative caller, releases this host's
	// control lock and listening socket. No unit/CLI task/model was started.
	os.Exit(0)
}
func TestBootstrapActualHostDeathRestoresAuthenticationAndListener(t *testing.T) {
	b, _, identity := authFixture(t)
	root := privateMaterialRoot(t)
	before, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	oldPublic := before.Public()
	oldDirectory := before.DirectoryIdentity()
	before.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBootstrapHostDeathHelper$")
	cmd.Env = []string{"KAIWU_BOOTSTRAP_HOST_FIXTURE=1", "KAIWU_BOOTSTRAP_ROOT=" + root}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic host: %v %s", err, output)
	}
	after, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	listener, err := after.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if oldDirectory != after.DirectoryIdentity() {
		t.Fatal("host death changed mount inode")
	}
	key, err := after.SigningKey()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			var host *HostChannel
			host, err = AuthenticateHost(ctx, conn, oldPublic.Bootstrap, key, fixedVerifier{&fixedPeer{identity: identity}})
			if host != nil {
				host.Detach()
			}
		}
		done <- err
	}()
	conn, err := net.DialTimeout("unix", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := AuthenticateBridge(ctx, conn, oldPublic.Bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	bridge.Detach()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestListenerOwnsDirectoryDescriptorPastMaterialClose(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := m.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	m.Close()
	conn, err := net.DialTimeout("unix", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("listener lost its own address on material close", err)
	}
	defer conn.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	accepted.Close()
}
