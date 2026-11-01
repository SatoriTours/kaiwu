//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/projecttools"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestTerminalMembersRetainWorkspaceUntilLastExit(t *testing.T) {
	s, err := workbench.Open(filepath.Join(t.TempDir(), "state.db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Command(workbench.Command{Type: "add_project", OperationID: "terminal-members", Revision: s.Snapshot().Revision, Name: "terminals", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	if out, e := exec.Command("git", "init", "--initial-branch=main", p.Path).CombinedOutput(); e != nil {
		t.Fatalf("git init: %v %s", e, out)
	}
	m := New(context.Background(), s, Config{DataDir: t.TempDir()})
	defer m.Close()
	provider, ok := any(m).(interface {
		AcquireTerminalLease(context.Context, string, string) (preview.Lease, error)
	})
	if !ok {
		t.Fatal("multiple terminal leases are not supported")
	}
	terms := projecttools.NewTerminals(context.Background())
	defer terms.Close()
	l1, err := provider.AcquireTerminalLease(context.Background(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := terms.Start(p.ID, p.Path, l1)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := provider.AcquireTerminalLease(context.Background(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := terms.Start(p.ID, p.Path, l2)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("two terminals reused one process identity")
	}
	if _, err = m.AcquireToolLease(context.Background(), p.ID, ""); err == nil {
		t.Fatal("file writer acquired terminal workspace")
	}
	if err = terms.Stop(first.ID); err != nil {
		t.Fatal(err)
	}
	if !m.ToolWriteBlocked(p.ID, "") {
		t.Fatal("first exit released sibling's workspace")
	}
	if _, err = m.AcquireToolLease(context.Background(), p.ID, ""); err == nil {
		t.Fatal("AI/file writer admitted while sibling alive")
	}
	if err = terms.Input(second.ID, "printf 'SIBLING_ALIVE\\n'\n"); err != nil {
		t.Fatal(err)
	}
	if err = terms.Stop(second.ID); err != nil {
		t.Fatal(err)
	}
	if m.ToolWriteBlocked(p.ID, "") {
		t.Fatal("last exit did not release reservation")
	}
	l3, err := m.AcquireToolLease(context.Background(), p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	l3.(interface{ NoProcess() }).NoProcess()
	if err = l3.Release(true); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalDurableOwnerChecksEveryGroup(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	for _, groups := range []map[string]int{{"ended": 99999999, "live": cmd.Process.Pid}, {"unknown": 0}} {
		dir := t.TempDir()
		data, _ := json.Marshal(map[string]any{"Boot": strings.TrimSpace(string(boot)), "Group": 0, "Groups": groups})
		if err = os.WriteFile(filepath.Join(dir, "owner.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		lease, e := acquireLease(dir, "lock", "owner.json")
		if e == nil {
			_ = lease.release(true)
			t.Fatal("accepted live/unknown terminal owner")
		}
	}
}
