package preview

import (
	"os"
	"path/filepath"
)

// staticCandidates is the fixed, bounded set of conventional HTML entries.
// Discovery never walks the tree, so a task workspace cannot make the server
// traverse an arbitrary directory or read a machine path.
var staticCandidates = []string{"index.html", "public/index.html", "dist/index.html", "build/index.html"}

// discoverStatic returns the only unambiguously viewable static HTML entry in
// the task worktree. Zero or several candidates are reported as undetermined
// with a reason, never guessed: the caller must not open a page the task did
// not declare.
func discoverStatic(tree string) (entry, reason string, err error) {
	found := []string{}
	for _, rel := range staticCandidates {
		ok, e := regularInside(tree, rel)
		if e != nil {
			return "", "", e
		}
		if ok {
			found = append(found, rel)
		}
	}
	switch len(found) {
	case 1:
		return found[0], "", nil
	case 0:
		return "", "暂无网页效果：任务工作区没有可确定的 HTML 入口；已有运行结果仍可查看", nil
	default:
		return "", "暂无网页效果：任务工作区有多个 HTML 入口，无法确定该打开哪一个；已有运行结果仍可查看", nil
	}
}

// regularInside accepts only a regular file reached without any symlink in the
// candidate's own path segment, so a symlinked entry cannot escape the
// worktree or expose a service/private file.
func regularInside(tree, rel string) (bool, error) {
	name := filepath.Join(tree, filepath.FromSlash(rel))
	// A missing, unreadable or symlinked candidate is simply not a determined
	// entry; the caller reports it instead of opening something else.
	canonical, e := filepath.EvalSymlinks(name)
	if e != nil || canonical != name {
		return false, nil
	}
	info, e := os.Lstat(name)
	return e == nil && info.Mode().IsRegular(), nil
}
