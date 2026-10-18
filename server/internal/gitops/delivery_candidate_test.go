package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func candidateFixture(t *testing.T) (workbench.Project, workbench.Task, workbench.Execution) {
	t.Helper()
	p, task, x := fixture(t)
	git(t, x.Workspace, "config", "core.filemode", "true")
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature\n"), 0600)
	return p, task, x
}

func realRunner() Runner {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		return Run(ctx, dir, nil, args...)
	}
}

func assertUnchangedHead(t *testing.T, x workbench.Execution) {
	t.Helper()
	if head, _ := Head(context.Background(), x.Workspace, "HEAD"); head != x.BaseSHA {
		t.Fatalf("unexpected commit %s", head)
	}
}

func TestDeliveryCandidateChangedAfterCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("capture freezes working state", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		if c.HEAD != x.BaseSHA || c.Tree == "" || len(c.Fingerprint) != 64 || len(c.ProtectedFingerprint) != 64 {
			t.Fatalf("bad candidate %+v", c)
		}
		if len(c.Files) != 1 || c.Files[0].Path != "feature" {
			t.Fatalf("expected feature in files: %+v", c.Files)
		}
	})

	t.Run("commit verifies tree after check", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		sha, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner())
		if e != nil {
			t.Fatal(e)
		}
		if sha == x.BaseSHA {
			t.Fatal("commit did not advance HEAD")
		}
		if tree, e := output(ctx, x.Workspace, "rev-parse", "HEAD^{tree}"); e != nil || tree != c.Tree {
			t.Fatalf("committed tree %s not equal checked tree %s (%v)", tree, c.Tree, e)
		}
		status, e := Files(ctx, x.Workspace)
		if e != nil || len(status) != 0 {
			t.Fatalf("candidate left dirty files: %+v %v", status, e)
		}
	})

	t.Run("content change invalidates", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("changed\n"), 0600)
		if _, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner()); e == nil {
			t.Fatal("changed content committed")
		}
		assertUnchangedHead(t, x)
	})

	t.Run("staged change invalidates", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(x.Workspace, "extra"), []byte("staged\n"), 0600)
		git(t, x.Workspace, "add", "extra")
		if _, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner()); e == nil {
			t.Fatal("extra staged file committed")
		}
		assertUnchangedHead(t, x)
	})

	t.Run("mode change invalidates", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(filepath.Join(x.Workspace, "feature"), 0700); e != nil {
			t.Fatal(e)
		}
		if _, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner()); e == nil {
			t.Fatal("mode change committed")
		}
		assertUnchangedHead(t, x)
	})

	t.Run("symlink swap invalidates", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		if e := os.Remove(filepath.Join(x.Workspace, "feature")); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink("base.txt", filepath.Join(x.Workspace, "feature")); e != nil {
			t.Fatal(e)
		}
		if _, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner()); e == nil {
			t.Fatal("symlink committed")
		}
		assertUnchangedHead(t, x)
	})

	t.Run("sensitive file invalidates", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		c, e := CaptureCandidate(ctx, p, task, x)
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(filepath.Join(x.Workspace, ".env"), []byte("secret\n"), 0600)
		if _, e := CommitCandidate(ctx, x, c, "feature", "Fixture", "fixture@example.invalid", realRunner()); e == nil {
			t.Fatal("sensitive file committed")
		}
		assertUnchangedHead(t, x)
	})

	t.Run("sensitive files refused at capture", func(t *testing.T) {
		p, task, x := candidateFixture(t)
		os.WriteFile(filepath.Join(x.Workspace, "id_rsa"), []byte("key\n"), 0600)
		if _, e := CaptureCandidate(ctx, p, task, x); e == nil {
			t.Fatal("capture accepted sensitive file")
		}
		assertUnchangedHead(t, x)
	})
}
