// Package preview owns explicitly started, task-scoped HTTP previews.
package preview

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type manifest struct {
	Version int      `json:"version"`
	Command []string `json:"command"`
	Path    string   `json:"path"`
	// Kind selects the preview source. Empty means the v1 command preview;
	// "static" serves an existing HTML entry from the task worktree without
	// starting any process.
	Kind  string `json:"kind,omitempty"`
	Root  string `json:"root,omitempty"`
	Entry string `json:"entry,omitempty"`
	// Artifacts declares the only downloadable files of this preview. They are
	// explicit names in the worktree; nothing else is ever offered.
	Artifacts []artifactDecl `json:"artifacts,omitempty"`
}

// manifestInvalid is the user-facing manifest refusal. It never embeds the
// worktree path or a raw filesystem error: the message is shown in the task
// preview panel and must not disclose a server directory.
var manifestInvalid = errors.New("预览配置无效或不可读，请检查工作区的 kaiwu-preview.json")

// errManifestMissing reports only that the manifest directory entry itself does
// not exist. Every other failure (link, FIFO, directory, unreadable file) is
// authoritative and must never fall back to conventional discovery.
var errManifestMissing = errors.New("预览配置不存在")

func readManifest(tree string) (manifest, [32]byte, error) {
	var v manifest
	var hash [32]byte
	name := filepath.Join(tree, "kaiwu-preview.json")
	// Lstat does not follow links, so a dangling or escaping symlink is a
	// present-and-invalid manifest instead of a missing one.
	info, e := os.Lstat(name)
	if e != nil {
		if os.IsNotExist(e) {
			return v, hash, errManifestMissing
		}
		return v, hash, manifestInvalid
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return v, hash, errors.New("预览配置不能使用符号链接，且必须是普通文件")
	}
	f, e := openManifest(name)
	if e != nil {
		return v, hash, manifestInvalid
	}
	defer f.Close()
	info, e = f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return v, hash, errors.New("预览配置必须是 16 KiB 以内的普通文件")
	}
	b, e := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if e != nil || len(b) > 16<<10 {
		return v, hash, errors.New("预览配置过大或不可读")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return v, hash, errors.New("预览配置格式无效（需要 version 1、command 和 path）")
	}
	switch v.Version {
	case 1:
		// v1 keeps its exact command contract; static fields stay empty so
		// existing previews behave unchanged.
		if v.Kind != "" || v.Root != "" || v.Entry != "" || len(v.Artifacts) != 0 {
			return v, hash, errors.New("预览配置格式无效（需要 version 1、command 和 path）")
		}
		if e = checkCommand(v); e != nil {
			return v, hash, e
		}
	case 2:
		if v, e = checkStatic(v); e != nil {
			return v, hash, e
		}
	default:
		return v, hash, errors.New("预览配置格式无效（需要 version 1、command 和 path）")
	}
	hash = sha256.Sum256(b)
	return v, hash, nil
}

// checkCommand validates the v1 process contract. Approval is bound to the
// manifest hash, so a one-click start never widens a changed command.
func checkCommand(v manifest) error {
	if len(v.Command) == 0 || len(v.Command) > 64 {
		return errors.New("预览配置格式无效（需要 version 1、command 和 path）")
	}
	found, total := false, 0
	for _, arg := range v.Command {
		total += len(arg)
		if arg == "" || len(arg) > 2048 || !utf8.ValidString(arg) || strings.ContainsAny(arg, "\x00\r\n") {
			return errors.New("预览命令参数无效")
		}
		found = found || strings.Contains(arg, "{port}")
	}
	if !found || total > 8192 || strings.Contains(v.Command[0], "{port}") {
		return errors.New("预览命令必须包含受控 {port} 参数，且不超过 8 KiB")
	}
	return checkPath(v.Path)
}

// checkStatic validates the v2 static entry contract: no command, one
// worktree-relative HTML entry and an optional relative root inside the
// worktree. Absolute paths, dot segments and backslashes are refused so the
// entry can never name a machine path outside the task workspace.
func checkStatic(v manifest) (manifest, error) {
	if v.Kind != "static" && v.Kind != "files" {
		return v, errors.New("静态预览配置必须声明 kind=static 或 kind=files，且不能包含 command")
	}
	if len(v.Command) != 0 {
		return v, errors.New("静态预览配置必须声明 kind=static 或 kind=files，且不能包含 command")
	}
	if e := checkArtifacts(v.Artifacts); e != nil {
		return v, e
	}
	if v.Kind == "files" {
		// A files-only result has no page at all: it must not pretend to have an
		// entry, and it must declare at least one downloadable file.
		if v.Entry != "" || len(v.Artifacts) == 0 {
			return v, errors.New("仅文件产物需要声明 artifacts，且不能包含 entry")
		}
		v.Root = ""
		if v.Path == "" {
			v.Path = "/"
		}
		if e := checkPath(v.Path); e != nil {
			return v, e
		}
		return v, nil
	}
	if v.Root == "" {
		v.Root = "."
	}
	if !relativeInside(v.Root) {
		return v, errors.New("静态预览 root 必须是工作区内的相对目录")
	}
	if v.Entry == "" || !strings.HasSuffix(strings.ToLower(v.Entry), ".html") || !relativeInside(v.Entry) {
		return v, errors.New("静态预览 entry 必须是工作区内的相对 HTML 路径")
	}
	if v.Path == "" {
		v.Path = "/"
	}
	if e := checkPath(v.Path); e != nil {
		return v, e
	}
	return v, nil
}

// relativeInside accepts only clean relative paths that stay inside the tree.
func relativeInside(name string) bool {
	if name == "." {
		return true
	}
	if name == "" || len(name) > 2048 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00\r\n") || strings.HasPrefix(name, "/") || filepath.IsAbs(name) || !filepath.IsLocal(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func checkPath(p string) error {
	u, e := url.Parse(p)
	if e != nil || len(p) > 2048 || strings.ContainsAny(p, "\\\r\n\x00") || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.User != nil || !safePath(u.Path) {
		return errors.New("预览 path 必须是站内路径")
	}
	return nil
}

func safePath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
