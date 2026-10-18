package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProtectedDeliveryFingerprint hashes the protected policy scope whose change
// must invalidate running delivery cycles. It is deterministic across
// JSON round-trips and excludes the mutable version counter, so an
// idempotent re-save of unchanged content keeps cycles alive.
func ProtectedDeliveryFingerprint(p ProjectDeliveryPolicy) string {
	if p.Checks == nil {
		p.Checks = []DeliveryCheck{}
	}
	if p.TaskWorkflows == nil {
		p.TaskWorkflows = []DeliveryWorkflow{}
	}
	if p.MainWorkflows == nil {
		p.MainWorkflows = []DeliveryWorkflow{}
	}
	p.Version = 0
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func DefaultDeliveryPolicy(projectID string) ProjectDeliveryPolicy {
	return ProjectDeliveryPolicy{ProjectID: projectID, RequireAcceptance: true, MaxRepairs: 3, ActiveSeconds: 3600, TriggerSeconds: 300, CISeconds: 1800}
}

var githubName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)

// CanonicalGitHubURL accepts only a single, credential-free GitHub repository.
// SSH user "git" is a protocol identity, not a password-bearing URL user.
func CanonicalGitHubURL(raw string) (string, error) {
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "ssh://git@github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return "", Invalid("仅支持明确的 GitHub.com 仓库地址")
	}
	switch u.Scheme {
	case "https":
		if u.User != nil {
			return "", Invalid("仓库地址不能包含凭据")
		}
	case "ssh":
		if u.User == nil || u.User.Username() != "git" {
			return "", Invalid("SSH 必须使用 git@github.com")
		}
		if _, ok := u.User.Password(); ok {
			return "", Invalid("仓库地址不能包含密码")
		}
	default:
		return "", Invalid("不支持的 Git 远程协议")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", Invalid("仓库地址必须包含 owner/repo")
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, p := range parts {
		if !githubName.MatchString(p) || strings.HasSuffix(p, ".") || strings.HasSuffix(p, ".lock") {
			return "", Invalid("仓库名称无效")
		}
	}
	path := strings.Join(parts, "/") + ".git"
	if u.Scheme == "ssh" {
		return "ssh://git@github.com/" + path, nil
	}
	return "https://github.com/" + path, nil
}

func ValidDeliveryBranch(branch string) bool {
	if branch == "" || len(branch) > 200 || branch == "@" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "refs/") || strings.HasPrefix(branch, "kaiwu/task-") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.ContainsAny(branch, " ~^:?*[\\\x00\r\n\t") {
		return false
	}
	for _, r := range branch {
		if r < 32 || r == 127 {
			return false
		}
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func ValidateDeliveryPolicy(p ProjectDeliveryPolicy) error {
	bad := func(s string) error { return &Error{"delivery_unconfigured", s} }
	if !validText(p.ProjectID, 100) || p.MaxRepairs < 1 || p.MaxRepairs > 100 {
		return bad("项目或有限修复额度无效")
	}
	for _, v := range []int64{p.ActiveSeconds, p.TriggerSeconds, p.CISeconds} {
		if v <= 0 || v > int64((7*24*time.Hour)/time.Second) {
			return bad("交付期限必须在 1 秒至 7 天内")
		}
	}
	// Draft policies are also persisted/projected. Never permit embedded URL
	// credentials merely because remote execution is currently disabled.
	if p.Repository.URL != "" {
		if _, err := CanonicalGitHubURL(p.Repository.URL); err != nil {
			return err
		}
	}
	if !p.Enabled {
		return nil
	}
	if !p.RevokedAt.IsZero() {
		return &Error{"authorization_revoked", "自动交付授权已撤销"}
	}
	if !validText(p.AuthorizationDigest, 256) || p.AuthorizedAt.IsZero() || !validText(p.Actor, 100) {
		return bad("请明确确认本项目远程写入的授权范围")
	}
	repo := p.Repository
	canonical, err := CanonicalGitHubURL(repo.URL)
	if err != nil {
		return err
	}
	if repo.ID <= 0 || !githubName.MatchString(repo.Owner) || !githubName.MatchString(repo.Name) || !ValidDeliveryBranch(repo.MainBranch) {
		return bad("仓库身份或主分支无效")
	}
	u, _ := url.Parse(canonical)
	if !strings.EqualFold(u.Path, "/"+repo.Owner+"/"+repo.Name+".git") {
		return bad("仓库地址与授权仓库不一致")
	}
	if p.APICredentialRef != "server_github_api" || (p.GitCredentialRef != "server_github_https" && p.GitCredentialRef != "server_github_ssh") {
		return bad("请选用服务器已配置的 GitHub 凭据引用")
	}
	if (strings.HasPrefix(canonical, "ssh:") && p.GitCredentialRef != "server_github_ssh") || (strings.HasPrefix(canonical, "https:") && p.GitCredentialRef != "server_github_https") {
		return bad("Git 认证方式与远程协议不匹配")
	}
	if len(p.Checks) == 0 || len(p.Checks) > 32 {
		return bad("请配置至少一项必需本地检查，最多 32 项")
	}
	ids := map[string]bool{}
	for _, c := range p.Checks {
		if !validText(c.ID, 100) || ids[c.ID] || len(c.Argv) == 0 || len(c.Argv) > 128 || c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 604800 {
			return bad("检查标识、命令或超时无效")
		}
		ids[c.ID] = true
		if c.Dir == "" || !filepath.IsLocal(c.Dir) || strings.ContainsAny(c.Dir, "\\\x00") {
			return bad("检查目录必须位于任务工作区内")
		}
		for i, arg := range c.Argv {
			if len(arg) > 8192 || strings.ContainsRune(arg, 0) || (i == 0 && strings.TrimSpace(arg) == "") {
				return bad("检查命令参数无效")
			}
		}
	}
	for _, set := range [][]DeliveryWorkflow{p.TaskWorkflows, p.MainWorkflows} {
		if len(set) == 0 || len(set) > 32 {
			return bad("任务和主分支均须配置必需 workflow")
		}
		seen := map[int64]bool{}
		for _, w := range set {
			if w.ID <= 0 || seen[w.ID] || len(w.Jobs) > 256 {
				return bad("workflow 标识无效或重复")
			}
			seen[w.ID] = true
			jobs := map[string]bool{}
			for _, job := range w.Jobs {
				if !validText(job, 300) || jobs[job] || strings.ContainsAny(job, "\x00\r\n") {
					return bad("必需 job 名称无效或重复")
				}
				jobs[job] = true
			}
		}
	}
	return nil
}
