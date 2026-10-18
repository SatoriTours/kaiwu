package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryCleanCandidateStillRejectsPrivateHistory(t *testing.T) {
	p, task, x := candidateFixture(t)
	git(t, x.Workspace, "add", "feature")
	git(t, x.Workspace, "commit", "-m", "feature")
	if err := os.WriteFile(filepath.Join(x.Workspace, ".env"), []byte("SYNTHETIC_ONLY=not-a-credential\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, x.Workspace, "add", ".env")
	git(t, x.Workspace, "commit", "-m", "private file")
	git(t, x.Workspace, "rm", ".env")
	git(t, x.Workspace, "commit", "-m", "remove private file")
	if _, err := CaptureCandidate(context.Background(), p, task, x); err == nil {
		t.Fatal("clean final tree allowed upload of private reachable history")
	}
}

func TestDeliveryCommitRejectsIndexOnlyChangeAfterAdd(t *testing.T) {
	p, task, x := candidateFixture(t)
	ctx := context.Background()
	c, err := CaptureCandidate(ctx, p, task, x)
	if err != nil {
		t.Fatal(err)
	}
	blobPath := filepath.Join(t.TempDir(), "blob")
	if err = os.WriteFile(blobPath, []byte("unchecked staged blob\n"), 0600); err != nil {
		t.Fatal(err)
	}
	blob := git(t, x.Workspace, "hash-object", "-w", blobPath)
	runner := Runner(func(ctx context.Context, dir string, args ...string) (string, error) {
		result, e := Run(ctx, dir, nil, args...)
		if e == nil && len(args) > 0 && args[0] == "add" {
			git(t, dir, "update-index", "--cacheinfo", "100644", blob, "base.txt")
		}
		return result, e
	})
	if _, err = CommitCandidate(ctx, x, c, "candidate", "Fixture", "fixture@example.invalid", runner); err == nil {
		t.Fatal("committed unchecked index-only content of an otherwise clean file")
	}
	assertUnchangedHead(t, x)
}

func TestDeliveryOptionalCommitIdentityKeepsExistingFallback(t *testing.T) {
	p, task, x := candidateFixture(t)
	ctx := context.Background()
	c, err := CaptureCandidate(ctx, p, task, x)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CommitCandidate(ctx, x, c, "candidate", "", "", realRunner()); err != nil {
		t.Fatalf("ordinary project without advanced identity cannot commit: %v", err)
	}
	if got := git(t, x.Workspace, "show", "-s", "--format=%an <%ae>", "HEAD"); got != "Kaiwu <kaiwu@localhost>" {
		t.Fatalf("unexpected identity %q", got)
	}
}
