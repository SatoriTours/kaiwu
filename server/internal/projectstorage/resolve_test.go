package projectstorage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectManagedAndLegacyKeepDistinctRoots(t *testing.T) {
	root := t.TempDir()
	l, err := Create(context.Background(), root, "project-A", "项目-A")
	if err != nil {
		t.Fatal(err)
	}
	p := ProjectRef{ID: "project-A", StorageMode: "managed", StorageRoot: l.Root, DirectoryName: "项目-A", LayoutVersion: 1, Path: l.Repo}
	got, err := ResolveProject(p, filepath.Join(root, "global"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != filepath.Join(l.Private, "sessions") || got.Attachments != filepath.Join(l.Private, "artifacts", "attachments") || got.Worktrees != l.Worktrees || got.Snapshots != filepath.Join(l.Private, "snapshots") {
		t.Fatalf("wrong roots: %+v", got)
	}
	legacy, err := ResolveProject(ProjectRef{ID: "legacy", Path: "/original/repo"}, filepath.Join(root, "global"))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Repo != "/original/repo" || legacy.Sessions != filepath.Join(root, "global", "sessions") || legacy.Worktrees != filepath.Join(root, "global", "worktrees") || legacy.Attachments != filepath.Join(root, "global", "attachments") {
		t.Fatalf("legacy moved: %+v", legacy)
	}
	for _, bad := range []ProjectRef{
		{ID: "project-B", StorageMode: "managed", StorageRoot: l.Root, DirectoryName: p.DirectoryName, LayoutVersion: 1, Path: l.Repo},
		{ID: p.ID, StorageMode: "managed", StorageRoot: l.Root, DirectoryName: p.DirectoryName, LayoutVersion: 2, Path: l.Repo},
		{ID: p.ID, StorageMode: "managed", StorageRoot: l.Root, DirectoryName: p.DirectoryName, LayoutVersion: 1, Path: root},
		{ID: p.ID, StorageMode: "mystery", Path: l.Repo},
	} {
		if _, err := ResolveProject(bad, root); err == nil {
			t.Fatalf("accepted bad identity: %+v", bad)
		}
	}
	if err := os.Rename(filepath.Join(l.Private, "sessions"), filepath.Join(l.Private, "saved-sessions")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(l.Private, "sessions")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProject(p, root); err == nil {
		t.Fatal("accepted replaced private directory")
	}
}
