package githubauth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// gitSSH builds the controlled SSH credential: an explicitly allowed key
// path or agent socket, non-interactive, strict host checking, existing
// known_hosts only. Unknown hosts are never auto-trusted.
func (r *Resolver) gitSSH(canonicalURL string) (GitCredential, error) {
	cred := GitCredential{}
	canonical, err := workbench.CanonicalGitHubURL(canonicalURL)
	if err != nil || !strings.HasPrefix(canonical, "ssh://git@github.com/") {
		return cred, fmt.Errorf("远程地址 %q 不是受支持的 github.com SSH 地址", canonicalURL)
	}
	key, hasKey := r.lookup(envSSHKey)
	agentAllowed, hasAgentFlag := r.lookup(envSSHAgentOK)
	if (!hasKey || key == "") && (!hasAgentFlag || agentAllowed != "allowed") {
		return cred, errors.New("服务未配置 GitHub SSH 凭据")
	}
	// Ignore both personal and system SSH config: ProxyCommand/Match exec must
	// not alter the frozen target or execute another credential-bearing child.
	// Existing known_hosts files remain in force; unknown hosts still fail.
	command := "ssh -F none -o BatchMode=yes -o StrictHostKeyChecking=yes -o HostName=github.com -o User=git -o ForwardAgent=no -o ClearAllForwardings=yes -o PreferredAuthentications=publickey"
	if hasKey && key != "" {
		if !filepath.IsAbs(key) || strings.Contains(key, "${") || strings.ContainsRune(key, '\x00') {
			return cred, errors.New("SSH key 路径必须为绝对路径")
		}
		if strings.Contains(key, "%") {
			return cred, errors.New("SSH key 路径不支持 %，请使用不含 % 的文件路径")
		}
		if info, e := os.Stat(key); e != nil || !info.Mode().IsRegular() {
			return cred, errors.New("SSH key 文件不可用")
		}
		// Git invokes GIT_SSH_COMMAND through a shell. Keep the approved path
		// one literal shell argument; unsupported OpenSSH tokens were rejected.
		command += " -o IdentitiesOnly=yes -o IdentityAgent=none -i " + quoteHelper(key)
	} else {
		sock, ok := r.lookup("SSH_AUTH_SOCK")
		if !ok || sock == "" {
			return cred, errors.New("显式允许的 SSH agent 不可用")
		}
		cred.Env = append(cred.Env, "SSH_AUTH_SOCK="+sock)
		command += " -o IdentitiesOnly=no -o IdentityFile=none -o IdentityAgent=SSH_AUTH_SOCK"
	}
	cred.Env = append(cred.Env, "GIT_SSH_VARIANT=ssh", "GIT_SSH_COMMAND="+command)
	cred.Close = func() error { return nil }
	return cred, nil
}
