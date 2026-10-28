// Package projecttools owns bounded file access and manual project processes.
package projecttools

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const MaxFile = 8 << 20

type Entry struct {
	ModifiedAt string `json:"modifiedAt"`
	Name       string `json:"name"`
	Directory  bool   `json:"directory"`
	Size       int64  `json:"size"`
}
type Files struct{ root *os.Root }

func Open(dir string) (*Files, error) {
	r, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	return &Files{r}, nil
}
func (f *Files) Close() error { return f.root.Close() }
func (f *Files) check(p string) error {
	if p == "." {
		return nil
	}
	if !fs.ValidPath(p) || strings.ContainsAny(p, "\\:\x00") {
		return errors.New("路径无效")
	}
	current := ""
	for _, part := range strings.Split(p, "/") {
		if strings.EqualFold(part, ".git") {
			return errors.New("不能访问 Git 内部文件")
		}
		current = path.Join(current, part)
		st, e := f.root.Lstat(current)
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("不能访问符号链接")
		}
	}
	return nil
}
func (f *Files) List(p string) ([]Entry, error) {
	if e := f.check(p); e != nil {
		return nil, e
	}
	dir, e := f.root.Open(p)
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	entries, e := dir.ReadDir(2001)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 2000 {
		return nil, errors.New("目录超过 2000 项，请使用更小的子目录")
	}
	result := []Entry{}
	for _, v := range entries {
		if strings.EqualFold(v.Name(), ".git") || v.Type()&os.ModeSymlink != 0 {
			continue
		}
		st, e := v.Info()
		if e != nil {
			continue
		}
		if !st.IsDir() && !st.Mode().IsRegular() {
			continue
		}
		result = append(result, Entry{Name: v.Name(), Directory: st.IsDir(), Size: st.Size(), ModifiedAt: st.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Directory != result[j].Directory {
			return result[i].Directory
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}
func (f *Files) Read(p string) ([]byte, error) {
	if e := f.check(p); e != nil {
		return nil, e
	}
	before, e := f.root.Lstat(p)
	if e != nil || !before.Mode().IsRegular() {
		return nil, errors.New("不是普通文件")
	}
	v, e := f.root.Open(p)
	if e != nil {
		return nil, e
	}
	defer v.Close()
	st, e := v.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return nil, errors.New("不是普通文件")
	}
	b, e := io.ReadAll(io.LimitReader(v, MaxFile+1))
	if len(b) > MaxFile {
		return nil, errors.New("文件超过 8 MiB")
	}
	return b, e
}
func (f *Files) Mkdir(p string) error {
	if e := f.check(p); e != nil {
		return e
	}
	return f.root.Mkdir(p, 0755)
}
func (f *Files) Upload(p string, r io.Reader) error {
	if e := f.check(p); e != nil {
		return e
	}
	v, e := f.root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return errors.New("无法新建文件，可能已存在；不会覆盖")
	}
	ok := false
	defer func() {
		v.Close()
		if !ok {
			_ = f.root.Remove(p)
		}
	}()
	n, e := io.Copy(v, io.LimitReader(r, MaxFile+1))
	if e != nil {
		return e
	}
	if n > MaxFile {
		return errors.New("文件超过 8 MiB")
	}
	if e = v.Sync(); e != nil {
		return e
	}
	ok = true
	return nil
}
