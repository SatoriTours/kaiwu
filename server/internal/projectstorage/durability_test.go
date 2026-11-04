package projectstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateCannotSucceedBeforeFinalDurabilityBarrier(t *testing.T) {
	root := t.TempDir()
	barrierFailure := errors.New("injected fsync failure")
	called := false
	_, err := createDurable(context.Background(), root, "project-one", "durable", func(r *os.Root) error {
		called = true
		for _, path := range []string{"repo/.git/HEAD", "repo/.git/config", ".kaiwu/identity.json", ".kaiwu/config", "worktrees"} {
			if _, err := r.Lstat(path); err != nil {
				t.Fatalf("durability ran before layout complete: %s: %v", path, err)
			}
		}
		return barrierFailure
	})
	if !called || !errors.Is(err, barrierFailure) {
		t.Fatalf("success bypassed durability: called=%v err=%v", called, err)
	}
	if _, err := os.Stat(filepath.Join(root, "durable", ".kaiwu", "identity.json")); err != nil {
		t.Fatal("failure discarded identity")
	}
}
