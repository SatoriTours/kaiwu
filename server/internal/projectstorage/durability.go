package projectstorage

import (
	"context"
	"errors"
	"io"
	"os"
)

// syncTree is deliberately bounded: this is an unborn project, not a general
// repository fsck or backup. Unknown links/special files and unexpectedly large
// trees require inspection instead of traversing unowned resources.
func syncTree(ctx context.Context, r *os.Root, remaining *int, depth int) error {
	if depth > 16 || *remaining <= 0 {
		return errors.New("managed initialization tree is too deep")
	}
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(*remaining + 1)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > *remaining {
		return errors.New("managed initialization tree requires inspection")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if *remaining <= 0 {
			return errors.New("managed initialization tree requires inspection")
		}
		*remaining -= 1
		info, err := r.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in initialization tree")
		}
		if info.IsDir() {
			child, err := openDirectory(r, entry.Name(), false)
			if err != nil {
				return err
			}
			err = syncTree(ctx, child, remaining, depth+1)
			child.Close()
			if err != nil {
				return err
			}
		} else {
			if !info.Mode().IsRegular() {
				return errors.New("special file in initialization tree")
			}
			file, err := r.Open(entry.Name())
			if err != nil {
				return err
			}
			opened, statErr := file.Stat()
			if statErr != nil || !os.SameFile(info, opened) {
				file.Close()
				return errors.New("initialization file changed")
			}
			err = file.Sync()
			file.Close()
			if err != nil {
				return err
			}
		}
	}
	return syncDirectory(r)
}

func readyIdentity(r *os.Root, projectID string) bool {
	info, err := r.Lstat("repo-ready")
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(projectID)+1) {
		return false
	}
	f, err := r.Open("repo-ready")
	if err != nil {
		return false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return false
	}
	b := make([]byte, len(projectID)+1)
	n, err := f.Read(b)
	return err == nil && n == len(b) && string(b) == projectID+"\n"
}
