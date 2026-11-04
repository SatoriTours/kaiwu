// Package projectstorage owns managed project containers, not OS sandboxes.
package projectstorage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const LayoutVersion = 1

type Layout struct {
	Root, Repo, Worktrees, Private                      string
	Sessions, Attachments, Snapshots, Config, Logs, Tmp string
}

// ValidDirectoryName accepts a single portable Unicode path component.
// It deliberately does not transliterate display names or silently sanitize them.
func ValidDirectoryName(name string) bool {
	if !utf8.ValidString(name) || name == "" || len(name) > 160 || strings.TrimSpace(name) != name ||
		strings.HasSuffix(name, ".") || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune("/\\:*?\"<>|", r) {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		(base[3] >= '1' && base[3] <= '9') {
		return false
	}
	return true
}

func Resolve(root, directoryName string) (Layout, error) {
	if !filepath.IsAbs(root) || !ValidDirectoryName(directoryName) {
		return Layout{}, errors.New("invalid managed project path")
	}
	root = filepath.Clean(root)
	// Resolve is read-only; actual creation uses directory handles, not this check.
	path := root
	for {
		info, err := os.Lstat(path)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return Layout{}, errors.New("managed root must contain only real directories")
		}
		if err != nil && !os.IsNotExist(err) {
			return Layout{}, err
		}
		if filepath.Dir(path) == path {
			break
		}
		path = filepath.Dir(path)
	}
	container := filepath.Join(root, directoryName)
	info, err := os.Lstat(container)
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return Layout{}, errors.New("project container is not a real directory")
	}
	if err != nil && !os.IsNotExist(err) {
		return Layout{}, err
	}
	return Layout{Root: container, Repo: filepath.Join(container, "repo"), Worktrees: filepath.Join(container, "worktrees"), Private: filepath.Join(container, ".kaiwu")}, nil
}

// openDirectory rejects links (including in-root links) and checks that the
// opened inode is the one inspected. Root handles prevent traversal escape.
func openDirectory(parent *os.Root, name string, create bool) (*os.Root, error) {
	if create {
		err := parent.Mkdir(name, 0700)
		if err != nil && !os.IsExist(err) {
			return nil, err
		}
		// Persist each new ancestor before descending; a later SQLite commit must
		// not outlive a newly created projects root after power loss.
		if err == nil {
			if err := syncDirectory(parent); err != nil {
				return nil, err
			}
		}
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("symbolic or non-directory managed path")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		child.Close()
		return nil, errors.New("managed path changed while opening")
	}
	return child, nil
}

func openStorageRoot(path string, create bool) (*os.Root, error) {
	r, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, err := openDirectory(r, part, create)
		r.Close()
		if err != nil {
			return nil, err
		}
		r = next
	}
	return r, nil
}
