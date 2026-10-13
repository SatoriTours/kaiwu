package gitops

import (
	"context"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"strings"
)

func validBranch(ctx context.Context, source, branch string) bool {
	if branch == "" || strings.HasPrefix(branch, "kaiwu/task-") {
		return false
	}
	_, e := output(ctx, source, "check-ref-format", "refs/heads/"+branch)
	return e == nil
}
func branchExists(ctx context.Context, source, branch string) bool {
	_, e := output(ctx, source, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return e == nil
}
func DetectMain(ctx context.Context, source string) (string, error) {
	if remote, e := output(ctx, source, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); e == nil {
		if b, ok := strings.CutPrefix(remote, "refs/remotes/origin/"); ok && validBranch(ctx, source, b) && branchExists(ctx, source, b) {
			return b, nil
		}
	}
	for _, b := range []string{"main", "master"} {
		if branchExists(ctx, source, b) {
			return b, nil
		}
	}
	b, e := output(ctx, source, "symbolic-ref", "--quiet", "--short", "HEAD")
	if e == nil && validBranch(ctx, source, b) {
		return b, nil
	}
	return "", errors.New("无法识别主分支，请在项目高级设置中指定")
}
func ResolveMain(ctx context.Context, p workbench.Project) (string, error) {
	b := p.MainBranchOverride
	if b == "" {
		b = p.DefaultBranch
	}
	if b == "" {
		return DetectMain(ctx, p.Path)
	}
	if !validBranch(ctx, p.Path, b) {
		return "", errors.New("主分支设置无效")
	}
	if branchExists(ctx, p.Path, b) {
		return b, nil
	}
	head, e := output(ctx, p.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if e == nil && head == b && !p.MainSeen && p.MainBranchOverride == "" {
		refs, _ := output(ctx, p.Path, "for-each-ref", "--format=%(refname)", "refs/heads")
		onlyTasks := true
		for _, ref := range strings.Split(refs, "\n") {
			if ref != "" && !strings.HasPrefix(ref, "refs/heads/kaiwu/task-") {
				onlyTasks = false
			}
		}
		if onlyTasks {
			return b, nil
		}
	}
	return "", errors.New("主分支不存在，请核查项目高级设置")
}
func Head(ctx context.Context, dir, ref string) (string, error) {
	return output(ctx, dir, "rev-parse", "--verify", ref+"^{commit}")
}
func Ancestor(ctx context.Context, dir, ancestor, head string) (bool, error) {
	_, e := output(ctx, dir, "merge-base", "--is-ancestor", ancestor, head)
	if e == nil {
		return true, nil
	}
	if ExitCode(e) == 1 {
		return false, nil
	}
	return false, e
}
