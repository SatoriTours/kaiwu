package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Preview(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution, serviceRoots ...string) (workbench.MergePreview, error) {
	s, e := Check(ctx, p, t, x, serviceRoots...)
	if e != nil {
		return workbench.MergePreview{}, e
	}
	name, email := p.CommitName, p.CommitEmail
	if name == "" {
		name, _ = output(ctx, p.Path, "config", "--local", "--get", "user.name")
	}
	if email == "" {
		email, _ = output(ctx, p.Path, "config", "--local", "--get", "user.email")
	}
	if name == "" || email == "" {
		name, email = "Kaiwu", "kaiwu@localhost"
	}
	preview := workbench.MergePreview{ProjectID: p.ID, TaskID: t.ID, MainBranch: s.MainBranch, MainSHA: s.MainSHA, TaskSHA: s.TaskSHA, CommitName: name, CommitEmail: email, Files: s.DirtyFiles, NeedsCommit: s.State == "dirty", CanMerge: s.CanMerge, Reason: s.Reason}
	if !preview.CanMerge {
		return preview, nil
	}
	preview.Blobs, e = workingBlobs(ctx, x.Workspace, preview.Files)
	if e != nil {
		return preview, e
	}
	preview.Modes, e = workingModes(ctx, x.Workspace, preview.Files)
	if e != nil {
		return preview, e
	}
	h := sha256.New()
	b, _ := json.Marshal(preview)
	h.Write(b)
	index, e := Run(ctx, x.Workspace, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return preview, e
	}
	h.Write([]byte(index))
	var total int64
	for _, f := range preview.Files {
		if e = ctx.Err(); e != nil {
			return preview, e
		}
		path := filepath.Join(x.Workspace, f.Path)
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			h.Write([]byte("deleted\x00"))
			continue
		}
		if e != nil {
			return preview, e
		}
		total += info.Size()
		if info.Size() > 32<<20 || total > 128<<20 {
			return preview, errors.New("待提交文件过大，请先整理")
		}
		b, _ := json.Marshal([]any{f.Path, info.Mode(), info.Size()})
		h.Write(b)
		file, e := os.Open(path)
		if e != nil {
			return preview, e
		}
		n, e := io.Copy(h, io.LimitReader(file, 32<<20+1))
		file.Close()
		if e != nil || n != info.Size() {
			return preview, errors.New("文件在预检期间发生变化")
		}
	}
	preview.Fingerprint = hex.EncodeToString(h.Sum(nil))
	return preview, nil
}

func workingBlobs(ctx context.Context, dir string, files []workbench.GitFile) (map[string]string, error) {
	blobs := map[string]string{}
	for _, f := range files {
		path := filepath.Join(dir, f.Path)
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			blobs[f.Path] = ""
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Size() > 32<<20 {
			return nil, errors.New("待提交文件类型或大小不支持")
		}
		sha, e := output(ctx, dir, "hash-object", "--no-filters", "--", f.Path)
		if e != nil {
			return nil, e
		}
		blobs[f.Path] = sha
	}
	return blobs, nil
}

func workingModes(ctx context.Context, dir string, files []workbench.GitFile) (map[string]string, error) {
	index, e := Run(ctx, dir, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return nil, e
	}
	indexed := map[string]string{}
	for _, row := range strings.Split(index, "\x00") {
		meta, path, ok := strings.Cut(row, "\t")
		parts := strings.Fields(meta)
		if ok && len(parts) == 3 {
			indexed[path] = parts[0]
		}
	}
	filemode, _ := output(ctx, dir, "config", "--type=bool", "--get", "core.filemode")
	modes := map[string]string{}
	for _, f := range files {
		info, e := os.Lstat(filepath.Join(dir, f.Path))
		if os.IsNotExist(e) {
			modes[f.Path] = ""
			continue
		}
		if e != nil {
			return nil, e
		}
		mode := "100644"
		if filemode == "false" {
			if indexed[f.Path] != "" {
				mode = indexed[f.Path]
			}
		} else if info.Mode().Perm()&0111 != 0 {
			mode = "100755"
		}
		modes[f.Path] = mode
	}
	return modes, nil
}
