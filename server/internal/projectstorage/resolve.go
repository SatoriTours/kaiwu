package projectstorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ProjectRef prevents the storage layer from importing durable business state.
type ProjectRef struct {
	ID, Path, StorageMode, StorageRoot, DirectoryName string
	LayoutVersion                                     int
}

// ResolveProject never creates or migrates anything. Unknown modes fail closed.
func ResolveProject(p ProjectRef, legacyDataDir string) (Layout, error) {
	if p.StorageMode == "" || p.StorageMode == "legacy" {
		if !filepath.IsAbs(p.Path) || !filepath.IsAbs(legacyDataDir) {
			return Layout{}, errors.New("invalid legacy storage root")
		}
		return Layout{Repo: p.Path, Root: legacyDataDir, Worktrees: filepath.Join(legacyDataDir, "worktrees"), Sessions: filepath.Join(legacyDataDir, "sessions"), Attachments: filepath.Join(legacyDataDir, "attachments"), Snapshots: filepath.Join(legacyDataDir, "snapshots")}, nil
	}
	if p.StorageMode != "managed" || p.LayoutVersion != LayoutVersion || !filepath.IsAbs(p.StorageRoot) || filepath.Base(p.StorageRoot) != p.DirectoryName || !ValidDirectoryName(p.ID) {
		return Layout{}, errors.New("invalid managed storage identity")
	}
	l, err := Resolve(filepath.Dir(p.StorageRoot), p.DirectoryName)
	if err != nil {
		return Layout{}, err
	}
	if l.Root != p.StorageRoot || l.Repo != p.Path {
		return Layout{}, errors.New("managed repository identity mismatch")
	}
	r, err := openStorageRoot(l.Root, false)
	if err != nil {
		return Layout{}, err
	}
	defer r.Close()
	for _, name := range []string{"repo", "worktrees", ".kaiwu", ".kaiwu/config", ".kaiwu/snapshots", ".kaiwu/sessions", ".kaiwu/artifacts", ".kaiwu/logs", ".kaiwu/tmp"} {
		child, err := openRelative(r, name)
		if err != nil {
			return Layout{}, err
		}
		child.Close()
	}
	private, err := openRelative(r, ".kaiwu")
	if err != nil {
		return Layout{}, err
	}
	defer private.Close()
	info, err := private.Lstat("identity.json")
	if err != nil || !info.Mode().IsRegular() {
		return Layout{}, errors.New("missing managed ownership marker")
	}
	b, err := private.ReadFile("identity.json")
	if err != nil {
		return Layout{}, err
	}
	var got identity
	if json.Unmarshal(b, &got) != nil || got != (identity{p.ID, p.DirectoryName, p.LayoutVersion}) || !readyIdentity(private, p.ID) {
		return Layout{}, errors.New("managed ownership marker mismatch")
	}
	l.Config = filepath.Join(l.Private, "config")
	l.Snapshots = filepath.Join(l.Private, "snapshots")
	l.Sessions = filepath.Join(l.Private, "sessions")
	l.Attachments = filepath.Join(l.Private, "artifacts", "attachments")
	l.Logs = filepath.Join(l.Private, "logs")
	l.Tmp = filepath.Join(l.Private, "tmp")
	return l, nil
}
func openRelative(r *os.Root, name string) (*os.Root, error) {
	// Open component by component so even links pointing inside the root are rejected.
	path := filepath.Clean(name)
	parts := []string{}
	for path != "." {
		parts = append([]string{filepath.Base(path)}, parts...)
		path = filepath.Dir(path)
	}
	current, err := r.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		next, err := openDirectory(current, part, false)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}
func EnsureDir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute private directory required")
	}
	r, err := openStorageRoot(path, true)
	if err != nil {
		return err
	}
	return r.Close()
}
