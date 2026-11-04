package projectstorage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func create(t *testing.T, root, id, name string) (Layout, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Create(ctx, root, id, name)
}

func TestCreateMakesRealUncommittedRepoAndPrivateLayout(t *testing.T) {
	root := t.TempDir()
	got, err := create(t, root, "project-one", "中文项目")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"repo", "worktrees", ".kaiwu/config", ".kaiwu/snapshots", ".kaiwu/sessions", ".kaiwu/artifacts", ".kaiwu/logs", ".kaiwu/tmp"} {
		info, err := os.Stat(filepath.Join(got.Root, p))
		if err != nil || !info.IsDir() {
			t.Fatalf("%s: %v", p, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "git", "-C", got.Repo, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(b)) != "main" {
		t.Fatalf("branch: %s %v", b, err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", got.Repo, "rev-parse", "--verify", "HEAD").Run(); err == nil {
		t.Fatal("created a commit")
	}
	again, err := create(t, root, "project-one", "中文项目")
	if err != nil || again != got {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if _, err := create(t, root, "another-project", "中文项目"); err == nil {
		t.Fatal("took another project's directory")
	}
}

func TestCreateDoesNotAdoptOrDeleteUnknownDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "existing")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "keep")
	if err := os.WriteFile(note, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := create(t, root, "new-project", "existing"); err == nil {
		t.Fatal("adopted existing directory")
	}
	b, _ := os.ReadFile(note)
	if string(b) != "untouched" {
		t.Fatal("unknown content lost")
	}
	if _, err := os.Stat(filepath.Join(dir, ".kaiwu")); !os.IsNotExist(err) {
		t.Fatal("modified unknown directory")
	}
}

func TestCreateConcurrentReservationsNeverOverwrite(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Create(context.Background(), root, string(rune('a'+i)), "same")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}

func TestCreateRejectsSymlinksOnRetryWithoutWritingOutside(t *testing.T) {
	for _, target := range []string{"repo", "worktrees", ".kaiwu", ".kaiwu/config", ".kaiwu/identity.json"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			got, err := create(t, root, "project-one", "safe")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(got.Root, target)
			// Move only this test's material, then substitute a malicious link.
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			dest := outside
			if strings.HasSuffix(target, "json") {
				dest = filepath.Join(outside, "identity.json")
				if err := os.WriteFile(dest, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(dest, path); err != nil {
				t.Fatal(err)
			}
			if _, err := create(t, root, "project-one", "safe"); err == nil {
				t.Fatal("symlink accepted")
			}
			entries, _ := os.ReadDir(outside)
			if (strings.HasSuffix(target, "json") && len(entries) != 1) || (!strings.HasSuffix(target, "json") && len(entries) != 0) {
				t.Fatal("wrote outside root")
			}
		})
	}
}

func TestCreateDoesNotRegisterPartiallyInitializedGit(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Create(ctx, root, "project-one", "partial"); err == nil {
		t.Fatal("cancel ignored")
	}
	git := filepath.Join(root, "partial", "repo", ".git")
	if err := os.MkdirAll(git, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("ref: refs/heads/main\\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := create(t, root, "project-one", "partial"); err == nil {
		t.Fatal("incomplete Git registered as ready")
	}
}

func TestCreateRetryAfterCancelledGitRetainsIdentity(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Create(ctx, root, "project-one", "retry"); err == nil {
		t.Fatal("cancelled operation succeeded")
	}
	got, err := create(t, root, "project-one", "retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got.Repo, ".git")); err != nil {
		t.Fatal(err)
	}
}
