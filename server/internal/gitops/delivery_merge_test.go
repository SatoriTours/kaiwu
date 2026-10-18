package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", message)
	return git(t, dir, "rev-parse", "HEAD")
}

func writeRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}

// TestDeliveryRevalidateCandidateAfterMainMoves proves the candidate sync and
// main integration boundary: a moved main is merged into the task branch with
// an ordinary merge and full revalidation, conflicts are never auto-resolved,
// and the local main branch only fast-forwards onto a candidate that contains
// the exact verified main — unknown local ahead commits are never uploaded.
func TestDeliveryRevalidateCandidateAfterMainMoves(t *testing.T) {
	ctx := context.Background()

	t.Run("moved main is merged into the task branch and gates rerun", func(t *testing.T) {
		p, task, x := fixture(t)
		writeRepoFile(t, x.Workspace, "feature.txt", "feature\n")
		taskHead := commitAll(t, x.Workspace, "feature")
		writeRepoFile(t, p.Path, "main.txt", "main moved\n")
		main2 := commitAll(t, p.Path, "main moved")

		c, e := SyncCandidate(ctx, p, task, x, x.BaseSHA, realRunner())
		if e != nil {
			t.Fatal(e)
		}
		head, _ := Head(ctx, x.Workspace, "HEAD")
		if c.HEAD != head || c.HEAD == taskHead {
			t.Fatalf("candidate did not record the merge commit: %+v (task %s)", c, taskHead)
		}
		if ok, _ := Ancestor(ctx, x.Workspace, main2, c.HEAD); !ok {
			t.Fatal("candidate does not contain the moved main")
		}
		if ok, _ := Ancestor(ctx, x.Workspace, taskHead, c.HEAD); !ok {
			t.Fatal("candidate dropped the task work")
		}

		// Local main ahead of the expected verified remote SHA is unknown work.
		if _, e := IntegrateCandidate(ctx, p, c.HEAD, x.BaseSHA, realRunner()); e == nil {
			t.Fatal("unknown local ahead main was integrated and would be uploaded")
		}
		// Verified: local main equals the expected SHA and the candidate carries it.
		sha, e := IntegrateCandidate(ctx, p, c.HEAD, main2, realRunner())
		if e != nil {
			t.Fatal(e)
		}
		if sha != c.HEAD {
			t.Fatalf("integration produced %s, want %s", sha, c.HEAD)
		}
		if now := git(t, p.Path, "rev-parse", "HEAD"); now != c.HEAD {
			t.Fatalf("local main not fast-forwarded: %s", now)
		}
	})

	t.Run("conflicting main is never auto-resolved", func(t *testing.T) {
		p, task, x := fixture(t)
		writeRepoFile(t, x.Workspace, "conflict.txt", "task side\n")
		before := commitAll(t, x.Workspace, "task side")
		writeRepoFile(t, p.Path, "conflict.txt", "main side\n")
		commitAll(t, p.Path, "main side")

		if _, e := SyncCandidate(ctx, p, task, x, x.BaseSHA, realRunner()); e == nil || !strings.Contains(e.Error(), "冲突") {
			t.Fatalf("conflicting merge accepted: %v", e)
		}
		if after := git(t, x.Workspace, "rev-parse", "HEAD"); after != before {
			t.Fatalf("conflict rewrite the task branch: %s", after)
		}
		if _, e := Run(ctx, x.Workspace, nil, "rev-parse", "--verify", "MERGE_HEAD"); e == nil {
			t.Fatal("unresolved merge state left behind")
		}
	})

	t.Run("only a fast-forward of a verified main is allowed", func(t *testing.T) {
		p, _, x := fixture(t)
		writeRepoFile(t, x.Workspace, "feature.txt", "feature\n")
		taskHead := commitAll(t, x.Workspace, "feature")
		writeRepoFile(t, p.Path, "main.txt", "main moved\n")
		main2 := commitAll(t, p.Path, "main moved")

		// The plain task head does not contain main: no fast-forward possible.
		if _, e := IntegrateCandidate(ctx, p, taskHead, main2, realRunner()); e == nil {
			t.Fatal("non-fast-forward integration accepted")
		}
		if now := git(t, p.Path, "rev-parse", "HEAD"); now != main2 {
			t.Fatalf("main moved on a refused integration: %s", now)
		}
	})

	t.Run("a clean no-op sync keeps the current head", func(t *testing.T) {
		p, task, x := fixture(t)
		writeRepoFile(t, x.Workspace, "feature.txt", "feature\n")
		head := commitAll(t, x.Workspace, "feature")
		c, e := SyncCandidate(ctx, p, task, x, x.BaseSHA, realRunner())
		if e != nil {
			t.Fatal(e)
		}
		if c.HEAD != head {
			t.Fatalf("no-op sync moved HEAD: %s -> %s", head, c.HEAD)
		}
		if c.Tree == "" || c.Fingerprint == "" {
			t.Fatalf("candidate not frozen: %+v", c)
		}
	})
}
