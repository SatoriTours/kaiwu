package gitops

import (
	"context"
	"errors"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// SyncCandidate brings the task worktree onto the latest main with an
// ordinary merge and freezes the result as the candidate the delivery gates
// must re-run against. A main that is already contained is a no-op: the
// current workspace is captured unchanged. Conflicts are never auto-resolved:
// the merge is aborted and the worktree is handed back untouched.
func SyncCandidate(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution, baseSHA string, runner Runner) (Candidate, error) {
	if x.Workspace == "" {
		return Candidate{}, errors.New("任务执行缺少工作区")
	}
	main, err := ResolveMain(ctx, p)
	if err != nil {
		return Candidate{}, err
	}
	mainSHA, err := Head(ctx, p.Path, "refs/heads/"+main)
	if err != nil || mainSHA == "" {
		return Candidate{}, errors.New("主分支不可读，请核查项目设置")
	}
	head, _ := Head(ctx, x.Workspace, "HEAD")
	if baseSHA != "" {
		if ok, err := Ancestor(ctx, x.Workspace, baseSHA, head); err != nil || !ok {
			return Candidate{}, errors.New("任务分支基线已变化，不能自动重验")
		}
	}
	contained, err := Ancestor(ctx, x.Workspace, mainSHA, head)
	if err != nil {
		return Candidate{}, err
	}
	if !contained {
		if _, err := runner(ctx, x.Workspace, "-c", "user.name=Kaiwu Delivery", "-c", "user.email=delivery@kaiwu.invalid", "merge", "--no-ff", "--no-edit", "--no-gpg-sign", "--no-verify-signatures", mainSHA); err != nil {
			_, _ = runner(ctx, x.Workspace, "merge", "--abort")
			return Candidate{}, errors.New("主分支与任务分支冲突，自动合并被中止，请人工处理")
		}
	}
	return CaptureCandidate(ctx, p, t, x)
}

// IntegrateCandidate fast-forwards the project's local main branch onto the
// verified candidate SHA. It only moves when local main still equals the
// expected verified remote SHA and the candidate contains it, so unknown
// local ahead commits are never uploaded, overwritten or rebased away.
func IntegrateCandidate(ctx context.Context, p workbench.Project, candidateSHA, expectedMainSHA string, runner Runner) (string, error) {
	if candidateSHA == "" || expectedMainSHA == "" {
		return "", errors.New("缺少已验证的候选或主分支基线")
	}
	main, err := ResolveMain(ctx, p)
	if err != nil {
		return "", err
	}
	ref := "refs/heads/" + main
	now, err := Head(ctx, p.Path, ref)
	if err != nil {
		return "", errors.New("主分支不可读，请核查项目设置")
	}
	if now != expectedMainSHA {
		return "", errors.New("本地主分支领先或偏离已验证提交，未上传也未覆盖")
	}
	ff, err := Ancestor(ctx, p.Path, expectedMainSHA, candidateSHA)
	if err != nil {
		return "", err
	}
	if !ff {
		return "", errors.New("候选不包含已验证的主分支提交，不能快进集成")
	}
	if _, e := runner(ctx, p.Path, "merge", "--ff-only", "--no-edit", "--no-gpg-sign", "--no-verify-signatures", candidateSHA); e != nil {
		return "", errors.New("主分支快进未确认成功，请检查主目录 Git 状态；未自动重试")
	}
	out, err := Head(ctx, p.Path, ref)
	if err != nil || out != candidateSHA {
		return "", errors.New("主分支未到达已验证候选，未视为集成成功")
	}
	return out, nil
}
