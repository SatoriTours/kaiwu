package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type workspaceOwner struct {
	TaskID, Source, Branch, BaseSHA string
	Ready                           bool
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	b, e := cmd.Output()
	if len(args) > 0 && args[0] == "ls-files" {
		return string(b), e
	}
	return strings.TrimSpace(string(b)), e
}

func inside(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Worktrees are task-owned and never reset or removed during recovery.
func (m *Manager) prepareWorkspace(ctx context.Context, task workbench.Task, x workbench.Execution) (workbench.Execution, error) {
	if b, e := hex.DecodeString(task.ID); e != nil || len(b) != 16 {
		return x, errors.New("任务工作区标识无效")
	}
	source, e := filepath.EvalSymlinks(m.store.Snapshot().Projects[task.ProjectID].Path)
	if e != nil {
		return x, errors.New("项目目录不可用")
	}
	source, e = filepath.Abs(source)
	if e != nil {
		return x, e
	}
	root, e := gitOutput(ctx, source, "rev-parse", "--show-toplevel")
	if e != nil || root != source {
		return x, errors.New("项目不是 Git 工作目录根目录")
	}
	dataRoot, e := filepath.EvalSymlinks(m.cfg.DataDir)
	if e != nil {
		return x, e
	}
	dataRoot, e = filepath.Abs(dataRoot)
	if e != nil {
		return x, e
	}
	parent := filepath.Join(dataRoot, "worktrees", task.ID)
	expectedParent := parent
	if e = os.MkdirAll(parent, 0700); e != nil {
		return x, e
	}
	parent, e = filepath.EvalSymlinks(parent)
	if e != nil {
		return x, e
	}
	parent, e = filepath.Abs(parent)
	if e != nil {
		return x, e
	}
	if parent != expectedParent || parent == source {
		return x, errors.New("工作区路径已改变或覆盖项目目录")
	}
	tree := filepath.Join(parent, "tree")
	ownerFile := filepath.Join(parent, "owner.json")
	branch := "kaiwu/task-" + task.ID
	if b, err := os.ReadFile(ownerFile); err == nil {
		var owner workspaceOwner
		if json.Unmarshal(b, &owner) != nil || owner.TaskID != task.ID || owner.Source != source || owner.Branch != branch {
			return x, errors.New("任务工作区归属不匹配")
		}
		if !owner.Ready {
			return x, errors.New("工作区准备曾中断，请先检查保留的目录；未覆盖或清理已有文件")
		}
		if e = validateWorktree(ctx, source, tree, branch); e != nil {
			return x, e
		}
		x.Workspace, x.Branch, x.BaseSHA = tree, branch, owner.BaseSHA
		return x, nil
	} else if !os.IsNotExist(err) {
		return x, err
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		return x, errors.New("工作区路径已占用，未覆盖已有文件")
	}
	project, e := m.ensureMain(ctx, task.ProjectID)
	if e != nil {
		return x, e
	}
	main, e := gitops.ResolveMain(ctx, project)
	if e != nil {
		return x, e
	}
	sha, headErr := gitOutput(ctx, source, "rev-parse", "--verify", "refs/heads/"+main+"^{commit}")
	if headErr != nil {
		refs, err := gitOutput(ctx, source, "show-ref", "--head")
		if err == nil || refs != "" {
			return x, errors.New("无法确认项目初始提交")
		}
		sha = ""
	} else {
		dirty, err := gitOutput(ctx, source, "status", "--porcelain", "--untracked-files=no")
		if err != nil || dirty != "" {
			return x, errors.New("项目有未提交的已跟踪改动，请先处理后开始开发")
		}
	}
	files, e := workspaceInputs(ctx, source, parent, sha == "")
	if e != nil {
		return x, e
	}
	owner := workspaceOwner{TaskID: task.ID, Source: source, Branch: branch, BaseSHA: sha}
	b, _ := json.Marshal(owner)
	f, e := os.OpenFile(ownerFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return x, e
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return x, errors.New("无法保存任务工作区归属")
	}
	args := []string{"worktree", "add", "-b", branch, tree, sha}
	if sha == "" {
		args = []string{"worktree", "add", "--orphan", "-b", branch, tree}
	}
	if _, e = gitOutput(ctx, source, args...); e != nil {
		return x, errors.New("无法创建独立 Git worktree；空仓库需要支持 --orphan 的 Git，未自动提交或清理")
	}
	for _, name := range files {
		if e = copyInput(source, tree, name); e != nil {
			return x, e
		}
	}
	if e = validateWorktree(ctx, source, tree, branch); e != nil {
		return x, e
	}
	owner.Ready = true
	b, _ = json.Marshal(owner)
	if e = os.WriteFile(ownerFile+".tmp", b, 0600); e != nil {
		return x, e
	}
	if e = os.Rename(ownerFile+".tmp", ownerFile); e != nil {
		return x, e
	}
	x.Workspace, x.Branch, x.BaseSHA = tree, branch, sha
	return x, nil
}

func validateWorktree(ctx context.Context, source, tree, branch string) error {
	return gitops.ValidateWorktree(ctx, source, tree, branch)
}

func privateInput(name string) bool {
	for _, p := range strings.Split(filepath.ToSlash(name), "/") {
		p = strings.ToLower(p)
		if p == ".git" || p == ".kaiwu" || p == ".pi" || p == ".codex" || p == ".aws" || p == ".ssh" || p == "node_modules" || p == ".env" || strings.HasPrefix(p, ".env.") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") || strings.HasPrefix(p, "id_rsa") || strings.HasPrefix(p, "id_ed25519") {
			return true
		}
	}
	return false
}

func workspaceInputs(ctx context.Context, source, parent string, unborn bool) ([]string, error) {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	if unborn {
		args = append(args, "--cached")
	}
	listed, e := gitOutput(ctx, source, args...)
	if e != nil {
		return nil, errors.New("无法读取项目初始文件")
	}
	var result []string
	var total int64
	dataRoot := filepath.Dir(filepath.Dir(parent))
	for _, name := range strings.Split(listed, "\x00") {
		if name == "" || privateInput(name) {
			continue
		}
		path := filepath.Join(source, name)
		if !filepath.IsLocal(name) || !inside(source, path) {
			return nil, errors.New("项目包含不安全路径")
		}
		if inside(dataRoot, path) {
			continue
		}
		// Reject symlinks, including parent directories; never copy their targets.
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil || resolved != path {
			return nil, errors.New("项目初始文件包含符号链接，请先处理")
		}
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			return nil, errors.New("项目初始文件不是普通文件")
		}
		total += info.Size()
		if info.Size() > 32<<20 || total > 128<<20 || len(result) >= 10000 {
			return nil, errors.New("项目初始文件过多或过大，请先整理 Git 忽略规则")
		}
		result = append(result, name)
	}
	return result, nil
}

func copyInput(source, tree, name string) error {
	path := filepath.Join(source, name)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return errors.New("项目初始文件已改变")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != path {
		return errors.New("项目初始文件链接已改变")
	}
	in, e := os.Open(path)
	if e != nil {
		return e
	}
	defer in.Close()
	target := filepath.Join(tree, name)
	if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		return e
	}
	out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()&0700)
	if e != nil {
		return e
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, 32<<20+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil || n != info.Size() {
		return fmt.Errorf("项目初始文件复制未完成：%s", name)
	}
	return nil
}
