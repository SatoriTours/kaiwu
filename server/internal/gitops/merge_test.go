package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitRefusesUnreviewedStagedChange(t *testing.T) {
	_, _, x := fixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("reviewed"), 0600)
	runner := func(ctx context.Context, dir string, args ...string) (string, error) {
		out, e := Run(ctx, dir, nil, args...)
		if len(args) > 0 && args[0] == "add" && e == nil {
			os.WriteFile(filepath.Join(dir, "feature"), []byte("not reviewed"), 0600)
			git(t, dir, "add", ".")
		}
		return out, e
	}
	if _, e := CommitTask(ctx, x.Workspace, "feature", "Fixture", "fixture@example.invalid", runner); e == nil {
		t.Fatal("unreviewed staged change committed")
	}
	if head, _ := Head(ctx, x.Workspace, "HEAD"); head != x.BaseSHA {
		t.Fatal("unexpected commit")
	}
}

func TestReviewUnreviewedMode(t *testing.T) {
	p, task, x := fixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("reviewed"), 0600)
	proof, e := Preview(ctx, p, task, x)
	if e != nil {
		t.Fatal(e)
	}
	runner := func(ctx context.Context, dir string, args ...string) (string, error) {
		out, e := Run(ctx, dir, nil, args...)
		if len(args) > 0 && args[0] == "add" && e == nil {
			git(t, dir, "update-index", "--chmod=+x", "feature")
		}
		return out, e
	}
	if _, e := CommitTask(ctx, x.Workspace, "feature", "Fixture", "fixture@example.invalid", runner, proof); e == nil {
		t.Fatal("UNSAFE: mode changed after review committed")
	}
}
func TestReviewSignatureExternalProgram(t *testing.T) {
	p, task, x := fixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	git(t, x.Workspace, "add", ".")
	git(t, x.Workspace, "commit", "-m", "feature")
	raw := git(t, x.Workspace, "cat-file", "commit", "HEAD")
	raw = strings.Replace(raw, "\n\n", "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n ZmFrZQ==\n -----END PGP SIGNATURE-----\n\n", 1) + "\n"
	cmd := exec.Command("git", "-C", x.Workspace, "hash-object", "-t", "commit", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(raw)
	b, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	git(t, x.Workspace, "update-ref", "HEAD", strings.TrimSpace(string(b)))
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "fake-gpg")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700)
	git(t, p.Path, "config", "gpg.program", script)
	git(t, p.Path, "config", "merge.verifySignatures", "true")
	proof, e := Preview(ctx, p, task, x)
	if e != nil {
		t.Fatal(e)
	}
	if !proof.CanMerge {
		return
	}
	_, _ = MergeTask(ctx, p, proof, proof.TaskSHA, func(ctx context.Context, dir string, args ...string) (string, error) {
		return Run(ctx, dir, nil, args...)
	})
	if _, e := os.Stat(marker); e == nil {
		t.Fatal("UNSAFE: external gpg program executed")
	}
}
