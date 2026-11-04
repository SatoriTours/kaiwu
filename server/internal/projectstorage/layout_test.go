package projectstorage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveKeepsRepositorySeparateAndAcceptsUnicode(t *testing.T) {
	root := t.TempDir()
	got, err := Resolve(root, "开悟-demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(root, "开悟-demo") || got.Repo != filepath.Join(root, "开悟-demo", "repo") ||
		got.Private != filepath.Join(root, "开悟-demo", ".kaiwu") || got.Worktrees != filepath.Join(root, "开悟-demo", "worktrees") {
		t.Fatalf("layout: %+v", got)
	}
}

func TestResolveRejectsUnsafeAndReservedNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "/absolute", "a\x00b", "a\nb", "CON", "nul.txt", "COM1", "lpt9.log", "trailing.", "trailing ", " space", "a:b"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(t.TempDir(), name); err == nil {
				t.Fatal("unsafe name accepted")
			}
		})
	}
	if _, err := Resolve("relative", "safe"); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestResolveRejectsSymlinkProject(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "link"); err == nil {
		t.Fatal("symlink project accepted")
	}
}
