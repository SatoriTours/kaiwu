//go:build linux

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
)

// A live writer group may never be overwritten by a later launch of the same
// operation, and the durable marker must survive until the group really exits.
func TestLiveGroupCannotOverwriteOwnership(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	child, pid := spawnLeasedChild(t)
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	if e := lease.started(pid); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "kaiwu-merge-owner.json")
	before, e := os.ReadFile(marker)
	if e != nil {
		t.Fatal(e)
	}
	// A second launch would clobber the proof that this group is still running.
	if e := lease.attempt(); !errors.Is(e, errGroupUnproven) {
		t.Fatalf("unproven group admitted another launch: %v", e)
	}
	if e := lease.started(os.Getpid()); !errors.Is(e, errGroupUnproven) {
		t.Fatalf("unproven group accepted a new pid: %v", e)
	}
	after, e := os.ReadFile(marker)
	if e != nil || string(after) != string(before) {
		t.Fatalf("marker overwritten: %s %v", after, e)
	}
	if !lease.unproven() {
		t.Fatal("live group reported as proven")
	}
	if e := lease.release(true); e == nil {
		t.Fatal("confirmed release of a live group reported safe")
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatalf("live group lost ownership: %v", e)
	}
	if again, e := acquireProjectMergeLease(root); e == nil {
		_ = again.release(false)
		t.Fatal("second writer admitted while the group was alive")
	}
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
	if lease.unproven() {
		t.Fatal("exited group still reported unproven")
	}
}

// A confirmed exit may recycle the lease: successive Git commands stay possible.
func TestConfirmedExitRecyclesLease(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = lease.release(true) }()
	for i := 0; i < 2; i++ {
		child, pid := spawnLeasedChild(t)
		if e := lease.attempt(); e != nil {
			t.Fatal(e)
		}
		if e := lease.started(pid); e != nil {
			t.Fatal(e)
		}
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}
	if lease.unproven() {
		t.Fatal("confirmed exits left the lease unproven")
	}
	if e := lease.release(true); e != nil {
		t.Fatalf("proven exit could not release ownership: %v", e)
	}
	if _, e := os.Stat(filepath.Join(root, "kaiwu-merge-owner.json")); !os.IsNotExist(e) {
		t.Fatalf("proven release kept ownership: %v", e)
	}
}

// After the lease is closed, no callback may reopen or re-record ownership.
func TestClosedLeaseRefusesCallbacks(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	if e := lease.release(true); e != nil {
		t.Fatal(e)
	}
	if e := lease.attempt(); e == nil {
		t.Fatal("closed lease recorded a launch")
	}
	if e := lease.started(os.Getpid()); e == nil {
		t.Fatal("closed lease recorded a process")
	}
	if e := lease.noProcess(); e == nil {
		t.Fatal("closed lease accepted a no-process proof")
	}
	if _, e := os.Stat(filepath.Join(root, "kaiwu-merge-owner.json")); !os.IsNotExist(e) {
		t.Fatalf("closed lease recreated ownership: %v", e)
	}
}

// An explicit no-process proof may free an empty launch window, but it may never
// erase a group the lease really recorded.
func TestNoProcessOnlyClearsEmptyLaunch(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = lease.release(false) }()
	marker := filepath.Join(root, "kaiwu-merge-owner.json")
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	if e := lease.noProcess(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("empty window kept ownership: %v", e)
	}
	child, pid := spawnLeasedChild(t)
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	if e := lease.started(pid); e != nil {
		t.Fatal(e)
	}
	if e := lease.noProcess(); !errors.Is(e, errGroupUnproven) {
		t.Fatalf("no-process proof erased a live group: %v", e)
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatalf("live group lost ownership: %v", e)
	}
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
}

// A launch window that recorded no process is unknown, not empty: later
// launches are refused and the durable marker stays until a real proof.
func TestUnattributedLaunchKeepsOwnership(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "kaiwu-merge-owner.json")
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	// The child really exists but this lease could not attribute it.
	if e := lease.started(0); e == nil {
		t.Fatal("unattributable pid accepted")
	}
	if !lease.unproven() {
		t.Fatal("unattributed launch reported as proven")
	}
	if e := lease.attempt(); !errors.Is(e, errGroupUnproven) {
		t.Fatalf("unattributed window admitted another launch: %v", e)
	}
	if e := lease.started(os.Getpid()); !errors.Is(e, errGroupUnproven) {
		t.Fatalf("unattributed window accepted a pid: %v", e)
	}
	if e := lease.noProcess(); e == nil {
		t.Fatal("no-process proof erased an unattributed launch")
	}
	if e := lease.release(true); e == nil {
		t.Fatal("confirmed release of an unattributed launch reported safe")
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatalf("unattributed launch lost ownership: %v", e)
	}
	if other, e := acquireProjectMergeLease(root); e == nil {
		_ = other.release(false)
		t.Fatal("second writer admitted for an unattributed launch")
	}
}

// The remote runner must not invent a "no process" proof for an abstract
// transport: only the lease itself may close a launch window it recorded.
func TestTransportCannotClaimNoProcess(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	probe := &noProcessProbe{executionLease: lease}
	child, pid := spawnLeasedChild(t)
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	if e := lease.started(pid); e != nil {
		t.Fatal(e)
	}
	runner := gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(func(context.Context, string, gitops.RemoteTarget, []string, string, gitops.ProcessLease) (string, error) {
		return "", errors.New("transport failed")
	})})
	if _, e := runner.Observe(context.Background(), root, gitops.RemoteTarget{URL: "https://github.com/owner/repo.git", MainRef: "refs/heads/main"}, "refs/heads/main", "", probe); e == nil {
		t.Fatal("failing transport reported success")
	}
	if probe.calls.Load() != 0 {
		t.Fatal("runner claimed no-process without proof")
	}
	if !lease.unproven() {
		t.Fatal("transport failure erased a live group")
	}
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
}

// noProcessProbe counts NoProcess claims made through the optional interface.
type noProcessProbe struct {
	*executionLease
	calls atomic.Int32
}

func (p *noProcessProbe) NoProcess()           { p.calls.Add(1); p.executionLease.noProcess() }
func (p *noProcessProbe) File() *os.File       { return p.executionLease.inheritedFile() }
func (p *noProcessProbe) Attempt() error       { return p.executionLease.attempt() }
func (p *noProcessProbe) Started(id int) error { return p.executionLease.started(id) }
