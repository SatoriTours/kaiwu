package gitops

import (
	"context"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"reflect"
	"strings"
)

func CommitTask(ctx context.Context, workspace, message, name, email string, runner Runner, proofs ...workbench.MergePreview) (string, error) {
	if strings.ContainsAny(name+email, "\r\n\x00<>") || strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		return "", errors.New("提交身份无效，请在高级设置中修改")
	}
	files, e := Files(ctx, workspace)
	if e != nil {
		return "", e
	}
	expected, e := workingBlobs(ctx, workspace, files)
	if e != nil {
		return "", e
	}
	modes, e := workingModes(ctx, workspace, files)
	if e != nil {
		return "", e
	}
	before, e := Head(ctx, workspace, "HEAD")
	if e != nil {
		before = ""
	}
	if len(proofs) > 0 {
		proof := proofs[0]
		if !reflect.DeepEqual(expected, proof.Blobs) || !reflect.DeepEqual(modes, proof.Modes) || before != proof.TaskSHA {
			return "", errors.New("提交前文件或任务 HEAD 已变化，请重新审核")
		}
		expected = proof.Blobs
	}
	if _, e := runner(ctx, workspace, "add", "--all", "--", "."); e != nil {
		return "", errors.New("暂存任务文件失败，保留工作区供检查")
	}
	staged, e := Run(ctx, workspace, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return "", e
	}
	actual := map[string]string{}
	actualModes := map[string]string{}
	for _, row := range strings.Split(staged, "\x00") {
		meta, path, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		parts := strings.Fields(meta)
		if len(parts) != 3 || parts[2] != "0" {
			return "", errors.New("暂存区存在未解决条目")
		}
		actual[path] = parts[1]
		actualModes[path] = parts[0]
	}
	for path, sha := range expected {
		if actual[path] != sha || actualModes[path] != modes[path] {
			return "", errors.New("暂存内容与审核不一致，请重新检查；未创建提交")
		}
	}
	var changed string
	if before == "" {
		changed, e = Run(ctx, workspace, nil, "ls-files", "-z")
	} else {
		changed, e = Run(ctx, workspace, nil, "diff", "--cached", "--name-only", "--no-renames", "-z", "--")
	}
	if e != nil {
		return "", e
	}
	for _, path := range strings.Split(changed, "\x00") {
		if path != "" {
			if _, ok := expected[path]; !ok {
				return "", errors.New("暂存范围出现未经审核的新文件；未创建提交")
			}
		}
	}
	now, _ := Head(ctx, workspace, "HEAD")
	if now != before {
		return "", errors.New("任务提交在写入前已变化，请重新审核")
	}
	if _, e := runner(ctx, workspace, "-c", "user.name="+name, "-c", "user.email="+email, "commit", "--no-gpg-sign", "-m", message); e != nil {
		return "", errors.New("任务提交未确认成功，请检查工作区；未自动重试")
	}
	return Head(ctx, workspace, "HEAD")
}
func MergeTask(ctx context.Context, p workbench.Project, proof workbench.MergePreview, taskSHA string, runner Runner) (string, error) {
	if e := mainReady(ctx, p, proof.MainBranch); e != nil {
		return "", e
	}
	main, _ := Head(ctx, p.Path, "refs/heads/"+proof.MainBranch)
	if main != proof.MainSHA {
		return "", errors.New("主分支提交已变化，请重新审核")
	}
	if main != "" {
		ff, e := Ancestor(ctx, p.Path, main, taskSHA)
		if e != nil {
			return "", e
		}
		if !ff {
			if _, e = runner(ctx, p.Path, "merge-tree", "--write-tree", main, taskSHA); e != nil {
				return "", errors.New("合并预检存在冲突、无共同基线或当前 Git 不支持；任务提交已保留，请在 Git 中处理后重试")
			}
		}
	}
	if e := mainReady(ctx, p, proof.MainBranch); e != nil {
		return "", e
	}
	now, _ := Head(ctx, p.Path, "refs/heads/"+proof.MainBranch)
	if now != main {
		return "", errors.New("主分支在合并前发生变化，请重新审核")
	}
	_, e := runner(ctx, p.Path, "-c", "user.name="+proof.CommitName, "-c", "user.email="+proof.CommitEmail, "merge", "--ff", "--no-edit", "--no-gpg-sign", "--no-verify-signatures", "--strategy=ort", "-m", "Merge task "+proof.TaskID, taskSHA)
	if e != nil {
		return "", errors.New("实际合并未确认成功，请检查主目录 Git 状态；未自动清理或重试")
	}
	return Head(ctx, p.Path, "HEAD")
}
