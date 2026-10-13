package webapi

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Resolve existing ancestors without creating anything during request validation.
func canonicalProjectPath(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", workbench.Invalid("请填写服务器上的项目绝对路径")
	}
	p := filepath.Clean(raw)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				return "", workbench.Invalid("项目路径或其父路径不是目录")
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(p) == p {
			return "", workbench.Invalid("项目路径无法访问，请检查目录权限")
		}
		suffix = append(suffix, filepath.Base(p))
		p = filepath.Dir(p)
	}
}

var scpRemote = regexp.MustCompile(`^[^@\s/:]+@[^:\s/]+:[^\s]+$`)

func validRemote(remote string) bool {
	if remote == "" {
		return true
	}
	if len(remote) > 2048 || strings.ContainsAny(remote, " \t\r\n\x00") {
		return false
	}
	if scpRemote.MatchString(remote) {
		return true
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" || u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.User != nil {
		if _, password := u.User.Password(); password {
			return false
		}
		if u.Scheme != "ssh" {
			return false
		}
	}
	return u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "ssh"
}

func projectGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// No inherited repository overrides, user aliases, templates or credentials.
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GIT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

func prepareProject(ctx context.Context, c workbench.Command) error {
	if !validRemote(c.RemoteURL) {
		return workbench.Invalid("远程地址需为 HTTPS 或 SSH 仓库地址，请勿包含密码或令牌")
	}
	if c.InitializeGit != "" && c.InitializeGit != "on" {
		return workbench.Invalid("Git 初始化选项无效")
	}
	_, markerErr := os.Lstat(filepath.Join(c.Path, ".git"))
	exists := markerErr == nil
	if !exists && !os.IsNotExist(markerErr) {
		return workbench.Invalid("无法访问 Git 目录，请检查权限")
	}
	if exists {
		root, err := projectGit(ctx, c.Path, "rev-parse", "--show-toplevel")
		resolved, resolveErr := filepath.EvalSymlinks(root)
		if err != nil || resolveErr != nil || resolved != c.Path {
			return workbench.Invalid("该目录不是有效的 Git 工作目录")
		}
		if c.RemoteURL != "" {
			origin, err := projectGit(ctx, c.Path, "config", "--local", "--get", "remote.origin.url")
			if err == nil && origin != c.RemoteURL {
				return workbench.Conflict("已有 origin 远程地址，未覆盖；请留空以沿用现有配置")
			}
			if err == nil {
				return nil
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return workbench.Invalid("无法读取现有 Git 远程配置")
			}
		}
	} else {
		if err := os.MkdirAll(c.Path, 0755); err != nil {
			return workbench.Invalid("无法创建项目目录，请检查服务器路径和权限")
		}
		if _, err := projectGit(ctx, c.Path, "init", "--initial-branch=main", "--template="); err != nil {
			return workbench.Invalid("Git 初始化未完成，请检查服务器 Git 和目录权限；已有文件保留")
		}
	}
	if c.RemoteURL != "" {
		if _, err := projectGit(ctx, c.Path, "remote", "add", "origin", c.RemoteURL); err != nil {
			return workbench.Invalid("远程地址配置未完成；目录和 Git 仓库已保留，可检查后重试")
		}
	}
	return nil
}
