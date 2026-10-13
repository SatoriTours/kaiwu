package gitops

import (
	"context"
	"errors"
	"fmt"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func safeConfig(ctx context.Context, dir string) error {
	config, e := Run(ctx, dir, nil, "config", "--null", "--list")
	if e != nil {
		return errors.New("无法读取 Git 配置")
	}
	for _, row := range strings.Split(config, "\x00") {
		key, _, _ := strings.Cut(row, "\n")
		key = strings.ToLower(key)
		if strings.HasPrefix(key, "filter.") || strings.HasPrefix(key, "merge.") && strings.HasSuffix(key, ".driver") {
			return errors.New("仓库配置了外部 filter／merge driver，请先处理后再集成")
		}
		if strings.HasPrefix(key, "branch.") && strings.HasSuffix(key, ".mergeoptions") {
			return errors.New("仓库配置了分支 mergeOptions，请先处理后再集成")
		}
	}
	return nil
}
func unfinished(ctx context.Context, dir string) error {
	for _, name := range []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"} {
		path, e := output(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", name)
		if e != nil {
			return e
		}
		if _, e = os.Lstat(path); e == nil {
			return errors.New("存在未完成 Git 操作，请先处理")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
func Files(ctx context.Context, dir string) ([]workbench.GitFile, error) {
	if e := safeConfig(ctx, dir); e != nil {
		return nil, e
	}
	raw, e := Run(ctx, dir, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none", "--no-renames")
	if e != nil {
		return nil, errors.New("无法检查工作区修改")
	}
	files := []workbench.GitFile{}
	for _, row := range strings.Split(raw, "\x00") {
		if row == "" {
			continue
		}
		if len(row) < 4 {
			return nil, errors.New("Git 状态格式无效")
		}
		name := row[3:]
		if !utf8.ValidString(name) || !filepath.IsLocal(name) {
			return nil, errors.New("文件名无法安全显示或限定")
		}
		files = append(files, workbench.GitFile{Path: name, Status: row[:2]})
		if len(files) > 10000 {
			return nil, errors.New("文件超过检查上限")
		}
	}
	return files, nil
}
func mainReady(ctx context.Context, p workbench.Project, branch string) error {
	head, e := output(ctx, p.Path, "symbolic-ref", "--short", "HEAD")
	if e != nil || head != branch {
		return errors.New("项目目录当前不在主分支 " + branch + "，请先在 Git 中切换")
	}
	if e = unfinished(ctx, p.Path); e != nil {
		return e
	}
	files, e := Files(ctx, p.Path)
	if e != nil {
		return e
	}
	if len(files) != 0 {
		return errors.New("项目主目录有未提交文件，请先处理；未覆盖原文件")
	}
	return nil
}
func Check(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution, serviceRoots ...string) (s workbench.GitStatus, err error) {
	s = workbench.GitStatus{TaskID: t.ID, State: "unknown", DirtyFiles: []workbench.GitFile{}, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	fail := func(e error) (workbench.GitStatus, error) { s.Reason = e.Error(); return s, nil }
	branch, e := ResolveMain(ctx, p)
	if e != nil {
		return fail(e)
	}
	s.MainBranch = branch
	s.MainSHA, _ = Head(ctx, p.Path, "refs/heads/"+branch)
	if x.Workspace == "" {
		if x.Branch != "" || x.BaseSHA != "" {
			return fail(errors.New("执行工作区记录不完整"))
		}
		s.State = "not_started"
		return s, nil
	}
	if e = validateOwner(ctx, p, t, x); e != nil {
		return fail(e)
	}
	protected, e := privateScope(p, x, serviceRoots)
	if e != nil {
		return fail(e)
	}
	if e = unfinished(ctx, x.Workspace); e != nil {
		return fail(e)
	}
	s.TaskSHA, _ = Head(ctx, x.Workspace, "HEAD")
	s.DirtyFiles, e = Files(ctx, x.Workspace)
	if e != nil {
		return fail(e)
	}
	if len(s.DirtyFiles) > 0 {
		s.State = "dirty"
	} else if s.TaskSHA == "" || s.TaskSHA == x.BaseSHA {
		s.State = "no_changes"
	} else if s.MainSHA == "" {
		s.State = "unmerged"
	} else {
		merged, e := Ancestor(ctx, p.Path, s.TaskSHA, s.MainSHA)
		if e != nil {
			return fail(errors.New("无法核对提交合并关系"))
		}
		s.State = "unmerged"
		if merged {
			s.State = "merged"
		}
	}
	if s.State == "dirty" || s.State == "unmerged" {
		s.CanMerge = true
		if e = mainReady(ctx, p, branch); e != nil {
			s.CanMerge = false
			s.Reason = e.Error()
		}
		if e = committedScope(ctx, x.Workspace, x.BaseSHA, s.TaskSHA, protected...); e != nil {
			s.CanMerge = false
			s.Reason = e.Error()
		}
		if e = eligible(ctx, x.Workspace, s.DirtyFiles, protected...); e != nil {
			s.CanMerge = false
			s.Reason = e.Error()
		}
	}
	return s, nil
}
func eligible(ctx context.Context, dir string, files []workbench.GitFile, protected ...string) error {
	for _, file := range files {
		if PrivatePath(file.Path, protected...) {
			return fmt.Errorf("需先处理私密或服务文件：%s", file.Path)
		}
		if strings.Contains(file.Status, "U") || file.Status == "AA" || file.Status == "DD" {
			return errors.New("任务存在未解决冲突")
		}
		path := filepath.Join(dir, file.Path)
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("不支持一键提交特殊文件或嵌套仓库：%s", file.Path)
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil || resolved != path {
			return errors.New("待提交文件包含符号链接")
		}
	}
	staged, e := Run(ctx, dir, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return e
	}
	for _, entry := range strings.Split(staged, "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			return errors.New("任务包含子模块，请先在 Git 中处理集成")
		}
	}
	return nil
}

func committedScope(ctx context.Context, dir, base, head string, protected ...string) error {
	if head == "" || head == base {
		return nil
	}
	// A merge imports reachable history, including files removed by later commits.
	// Inspect every introduced commit rather than only the final tree difference.
	args := []string{"log", "--format=", "--name-only", "--root", "-m", "--no-renames", "--no-ext-diff", "--no-textconv", "-z", head}
	if base != "" {
		args = append(args, "--not", base)
	}
	args = append(args, "--")
	raw, e := Run(ctx, dir, nil, args...)
	if e != nil {
		return errors.New("无法核对任务已提交文件范围")
	}
	for _, name := range strings.Split(raw, "\x00") {
		if name == "" {
			continue
		}
		if !utf8.ValidString(name) || !filepath.IsLocal(name) || PrivatePath(name, protected...) {
			return errors.New("任务提交包含需先处理的私密文件或不安全路径")
		}
	}
	return nil
}
