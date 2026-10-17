// Package preview owns explicitly started, task-scoped HTTP previews.
package preview

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type manifest struct {
	Version int      `json:"version"`
	Command []string `json:"command"`
	Path    string   `json:"path"`
}

func readManifest(tree string) (manifest, [32]byte, error) {
	var v manifest
	var hash [32]byte
	name := filepath.Join(tree, "kaiwu-preview.json")
	canonical, e := filepath.EvalSymlinks(name)
	if e != nil {
		return v, hash, e
	}
	if canonical != name {
		return v, hash, errors.New("预览配置不能使用符号链接")
	}
	f, e := openManifest(name)
	if e != nil {
		return v, hash, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return v, hash, errors.New("预览配置必须是 16 KiB 以内的普通文件")
	}
	b, e := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if e != nil || len(b) > 16<<10 {
		return v, hash, errors.New("预览配置过大或不可读")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF || v.Version != 1 || len(v.Command) == 0 || len(v.Command) > 64 {
		return v, hash, errors.New("预览配置格式无效（需要 version 1、command 和 path）")
	}
	found, total := false, 0
	for _, arg := range v.Command {
		total += len(arg)
		if arg == "" || len(arg) > 2048 || !utf8.ValidString(arg) || strings.ContainsAny(arg, "\x00\r\n") {
			return v, hash, errors.New("预览命令参数无效")
		}
		found = found || strings.Contains(arg, "{port}")
	}
	if !found || total > 8192 || strings.Contains(v.Command[0], "{port}") {
		return v, hash, errors.New("预览命令必须包含受控 {port} 参数，且不超过 8 KiB")
	}
	u, e := url.Parse(v.Path)
	if e != nil || len(v.Path) > 2048 || strings.ContainsAny(v.Path, "\\\r\n\x00") || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.User != nil || !safePath(u.Path) {
		return v, hash, errors.New("预览 path 必须是站内路径")
	}
	hash = sha256.Sum256(b)
	return v, hash, nil
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
