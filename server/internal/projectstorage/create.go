package projectstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type identity struct {
	ProjectID     string `json:"projectId"`
	DirectoryName string `json:"directoryName"`
	LayoutVersion int    `json:"layoutVersion"`
}

func syncDirectory(r *os.Root) error {
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Create exclusively reserves a container. Only an exact identity permits
// retry. An incomplete reservation with no identity is retained, never adopted
// or deleted. The caller must durably reserve projectID before calling Create.
func Create(ctx context.Context, root, projectID, directoryName string) (Layout, error) {
	return createDurable(ctx, root, projectID, directoryName, func(r *os.Root) error {
		remaining := 512
		return syncTree(ctx, r, &remaining, 0)
	})
}

func createDurable(ctx context.Context, root, projectID, directoryName string, durability func(*os.Root) error) (Layout, error) {
	layout, err := Resolve(root, directoryName)
	if err != nil {
		return Layout{}, err
	}
	if !ValidDirectoryName(projectID) {
		return Layout{}, errors.New("invalid project identity")
	}
	// Pinned directory handles for Git are currently implemented on Linux only.
	// Never silently substitute a race-prone host path on another platform.
	if runtime.GOOS != "linux" {
		return Layout{}, errors.New("managed Git creation requires Linux directory handles")
	}
	parent, err := openStorageRoot(root, true)
	if err != nil {
		return Layout{}, err
	}
	defer parent.Close()
	err = parent.Mkdir(directoryName, 0700)
	fresh := err == nil
	if err != nil && !os.IsExist(err) {
		return Layout{}, err
	}
	container, err := openDirectory(parent, directoryName, false)
	if err != nil {
		return Layout{}, err
	}
	defer container.Close()
	private, err := openDirectory(container, ".kaiwu", fresh)
	if err != nil {
		return Layout{}, fmt.Errorf("unowned or incomplete container: %w", err)
	}
	defer private.Close()
	want := identity{projectID, directoryName, LayoutVersion}
	if fresh {
		body, _ := json.Marshal(want)
		f, err := private.OpenFile("identity.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return Layout{}, err
		}
		_, err = f.Write(body)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return Layout{}, err
		}
		if closeErr != nil {
			return Layout{}, closeErr
		}
		for _, dir := range []*os.Root{private, container, parent} {
			if err := syncDirectory(dir); err != nil {
				return Layout{}, err
			}
		}
	} else {
		info, err := private.Lstat("identity.json")
		if err != nil || !info.Mode().IsRegular() {
			return Layout{}, errors.New("missing or unsafe project identity")
		}
		f, err := private.Open("identity.json")
		if err != nil {
			return Layout{}, err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(info, opened) {
			f.Close()
			return Layout{}, errors.New("project identity changed")
		}
		var got identity
		err = json.NewDecoder(f).Decode(&got)
		f.Close()
		if err != nil || got != want {
			return Layout{}, errors.New("directory belongs to another project or layout")
		}
	}
	expected := map[string]os.FileInfo{}
	for _, name := range []string{"config", "snapshots", "sessions", "artifacts", "logs", "tmp"} {
		dir, err := openDirectory(private, name, true)
		if err != nil {
			return Layout{}, err
		}
		info, err := dir.Stat(".")
		dir.Close()
		if err != nil {
			return Layout{}, err
		}
		expected[filepath.Join(".kaiwu", name)] = info
	}
	worktrees, err := openDirectory(container, "worktrees", true)
	if err != nil {
		return Layout{}, err
	}
	worktreeInfo, err := worktrees.Stat(".")
	worktrees.Close()
	if err != nil {
		return Layout{}, err
	}
	expected["worktrees"] = worktreeInfo
	repo, err := openDirectory(container, "repo", true)
	if err != nil {
		return Layout{}, err
	}
	defer repo.Close()
	// Never rerun Git against an existing repository's editable configuration.
	gitInfo, err := repo.Lstat(".git")
	if err == nil {
		if !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 {
			return Layout{}, errors.New("unsafe managed Git directory")
		}
		gitRoot, err := openDirectory(repo, ".git", false)
		if err != nil {
			return Layout{}, err
		}
		head, headErr := gitRoot.Lstat("HEAD")
		gitRoot.Close()
		if headErr != nil || !head.Mode().IsRegular() || !readyIdentity(private, projectID) {
			return Layout{}, errors.New("incomplete managed Git initialization; inspection required")
		}
	} else if os.IsNotExist(err) {
		if _, err := private.Lstat("repo-ready"); !os.IsNotExist(err) {
			return Layout{}, errors.New("managed repository disappeared; inspection required")
		}
		pinned, err := repo.Open(".")
		if err != nil {
			return Layout{}, err
		}
		defer pinned.Close()
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, "git", "init", "--initial-branch=main", "--template=", "--", "/proc/self/fd/3")
		cmd.ExtraFiles = []*os.File{pinned}
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "GIT_") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
		if err := cmd.Run(); err != nil {
			return Layout{}, fmt.Errorf("managed Git initialization incomplete: %w", err)
		}
	} else {
		return Layout{}, err
	}
	// Reopen the configured absolute root without creating anything, comparing
	// every pinned directory and each layout member before registering paths.
	now, err := openStorageRoot(root, false)
	if err != nil {
		return Layout{}, err
	}
	nowInfo, err := now.Stat(".")
	now.Close()
	parentInfo, parentErr := parent.Stat(".")
	if err != nil || parentErr != nil || !os.SameFile(nowInfo, parentInfo) {
		return Layout{}, errors.New("storage root changed during creation")
	}
	for name, handle := range map[string]*os.Root{".kaiwu": private, "repo": repo} {
		info, err := handle.Stat(".")
		if err != nil {
			return Layout{}, err
		}
		expected[name] = info
	}
	for name, want := range expected {
		actual, err := container.Lstat(name)
		if err != nil || actual.Mode()&os.ModeSymlink != 0 || !os.SameFile(actual, want) {
			return Layout{}, errors.New("managed layout changed during creation")
		}
	}
	// A rename/replacement cannot register a different path after handle-based I/O.
	current, err := parent.Lstat(directoryName)
	opened, openErr := container.Stat(".")
	if err != nil || openErr != nil || !os.SameFile(current, opened) || current.Mode()&os.ModeSymlink != 0 {
		return Layout{}, errors.New("project container changed during creation")
	}
	if filepath.Clean(layout.Root) != filepath.Join(filepath.Clean(root), directoryName) {
		return Layout{}, errors.New("invalid container root")
	}
	// All created files and directories must reach stable storage before SQLite
	// may publish a successful receipt. A missing marker after interruption
	// reports an incomplete Git initialization, never a false success.
	if err := durability(container); err != nil {
		return Layout{}, err
	}
	if !readyIdentity(private, projectID) {
		marker, err := private.OpenFile("repo-ready", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return Layout{}, err
		}
		_, err = marker.WriteString(projectID + "\n")
		if err == nil {
			err = marker.Sync()
		}
		closeErr := marker.Close()
		if err != nil {
			return Layout{}, err
		}
		if closeErr != nil {
			return Layout{}, closeErr
		}
	}
	for _, dir := range []*os.Root{private, container, parent} {
		if err := syncDirectory(dir); err != nil {
			return Layout{}, err
		}
	}
	return layout, nil
}
