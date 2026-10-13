//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMergeLeaseRefusesOrphanOrUnknownOwner(t *testing.T) {
	for _, group := range []int{0, syscall.Getpgrp()} {
		root := t.TempDir()
		boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(processOwner{Boot: strings.TrimSpace(string(boot)), Group: group})
		path := filepath.Join(root, "kaiwu-merge-owner.json")
		os.WriteFile(path, b, 0600)
		if lease, e := acquireProjectMergeLease(root); e == nil {
			lease.release(false)
			t.Fatal("unverified project writer accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(b) {
			t.Fatal("existing ownership overwritten")
		}
	}
}

func TestEveryGitLaunchDurablyResetsOwnership(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.release(true)
	if e = lease.started(2147483647); e != nil {
		t.Fatal(e)
	}
	lease.attempt()
	b, e := os.ReadFile(filepath.Join(root, "kaiwu-merge-owner.json"))
	if e != nil {
		t.Fatal(e)
	}
	var owner processOwner
	if e = json.Unmarshal(b, &owner); e != nil {
		t.Fatal(e)
	}
	if owner.Group != 0 {
		t.Fatal("new launch retained previous process identity", owner.Group)
	}
}

func TestIndependentConfiguredServiceData(t *testing.T) {
	m, task, x, _ := integrationFixture(t)
	m.cfg.DataDir = filepath.Join(x.Workspace, "state")
	session := filepath.Join(m.cfg.DataDir, "sessions", "fixture-conversation", "fixture.jsonl")
	if e := os.MkdirAll(filepath.Dir(session), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(session, []byte("fixture private session content"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e == nil && p.CanMerge {
		t.Fatalf("UNSAFE: configured service session included: %+v", p.Files)
	}
}

func TestIndependentRecoveryLiveWriter(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	proof, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "recovery-live", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: proof.ID, CommitMessage: "feature"})
	if e != nil {
		t.Fatal(e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "succeeded" {
		t.Fatal(op)
	}
	m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error { v.Phase = "needs_check"; return nil })
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	b, _ := json.Marshal(processOwner{Boot: strings.TrimSpace(string(boot)), Group: syscall.Getpgrp()})
	os.WriteFile(filepath.Join(source, ".git", "kaiwu-merge-owner.json"), b, 0600)
	m.RecoverMerges(context.Background())
	if got := m.store.Snapshot().Merges[op.ID]; got.Phase == "succeeded" {
		t.Fatal("UNSAFE: recovery marked success despite active recorded writer")
	}
}
