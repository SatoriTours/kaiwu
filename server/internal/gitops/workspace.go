package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ExitCode(e error) int {
	var x *exec.ExitError
	if errors.As(e, &x) {
		return x.ExitCode()
	}
	return -1
}
func CommonDir(ctx context.Context, source string) (string, error) {
	dir, e := output(ctx, source, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(dir)
}
func ValidateWorktree(ctx context.Context, source, tree, branch string) error {
	canonical, e := filepath.EvalSymlinks(tree)
	if e != nil || canonical != tree {
		return errors.New("任务工作区路径已改变")
	}
	root, e := output(ctx, tree, "rev-parse", "--show-toplevel")
	if e != nil || root != tree {
		return errors.New("任务工作区身份不匹配")
	}
	want, e := CommonDir(ctx, source)
	if e != nil {
		return e
	}
	got, e := CommonDir(ctx, tree)
	if e != nil || got != want {
		return errors.New("任务工作区不属于原项目")
	}
	ref, e := output(ctx, tree, "symbolic-ref", "--short", "HEAD")
	if e != nil || ref != branch {
		return errors.New("任务工作区分支已改变")
	}
	return nil
}
func validateOwner(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution) error {
	source, e := filepath.EvalSymlinks(p.Path)
	if e != nil || source != p.Path {
		return errors.New("项目目录路径已改变")
	}
	if x.Branch != "kaiwu/task-"+t.ID {
		return errors.New("任务分支归属不匹配")
	}
	var owner struct {
		TaskID, Source, Branch, BaseSHA string
		Ready                           bool
	}
	b, e := os.ReadFile(filepath.Join(filepath.Dir(x.Workspace), "owner.json"))
	if e != nil || json.Unmarshal(b, &owner) != nil || !owner.Ready || owner.TaskID != t.ID || owner.Source != source || owner.Branch != x.Branch || owner.BaseSHA != x.BaseSHA {
		return errors.New("任务工作区归属记录不匹配")
	}
	return ValidateWorktree(ctx, source, x.Workspace, x.Branch)
}
func PrivatePath(name string, protected ...string) bool {
	for _, prefix := range protected {
		if name == prefix || strings.HasPrefix(name, prefix+string(filepath.Separator)) {
			return true
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		p := strings.ToLower(part)
		if p == ".git" || p == ".kaiwu" || p == ".pi" || p == ".codex" || p == ".aws" || p == ".ssh" || p == "node_modules" || p == ".env" || strings.HasPrefix(p, ".env.") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") || strings.HasPrefix(p, "id_rsa") || strings.HasPrefix(p, "id_ed25519") || p == "workbench.db" || p == "web-access-token" {
			return true
		}
	}
	return false
}

// Protect configured service data independently of its directory name, including
// tracked copies mapped from the project into a task worktree.
func privateScope(p workbench.Project, x workbench.Execution, roots []string) ([]string, error) {
	var protected []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		absolute, e := filepath.Abs(root)
		if e != nil {
			return nil, e
		}
		absolute, e = canonicalFuture(absolute)
		if e != nil {
			return nil, errors.New("无法核实服务数据路径")
		}
		if rel, e := filepath.Rel(x.Workspace, absolute); e == nil && (rel == "." || filepath.IsLocal(rel)) {
			return nil, errors.New("服务数据目录位于任务工作区内，请先迁移数据目录")
		}
		if rel, e := filepath.Rel(p.Path, absolute); e == nil {
			if rel == "." {
				return nil, errors.New("项目目录也是服务数据目录，不能一键集成")
			}
			if filepath.IsLocal(rel) {
				protected = append(protected, rel)
			}
		}
	}
	return protected, nil
}

func canonicalFuture(path string) (string, error) {
	resolved, e := filepath.EvalSymlinks(path)
	if !os.IsNotExist(e) {
		return resolved, e
	}
	parent, e := canonicalFuture(filepath.Dir(path))
	if e != nil {
		return "", e
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
