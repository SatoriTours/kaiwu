package githubauth

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// gitSSH builds the controlled SSH credential: an explicitly allowed key
// path or agent socket, non-interactive, strict host checking, existing
// known_hosts only. Unknown hosts are never auto-trusted.
const sshHost = "git@github.com:"

func (r *Resolver) gitSSH(canonicalURL string) (GitCredential, error) {
	cred := GitCredential{}
	if !strings.HasPrefix(canonicalURL, sshHost) {
		return cred, fmt.Errorf("远程地址 %q 不是受支持的 github.com SSH 地址", canonicalURL)
	}
	key, hasKey := r.lookup(envSSHKey)
	agentAllowed, hasAgentFlag := r.lookup(envSSHAgentOK)
	if (!hasKey || key == "") && (!hasAgentFlag || agentAllowed != "allowed") {
		return cred, errors.New("服务未配置 GitHub SSH 凭据")
	}
	command := "ssh -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes"
	if hasKey && key != "" {
		if !strings.HasPrefix(key, "/") {
			return cred, errors.New("SSH key 路径必须为绝对路径")
		}
		if _, e := os.Stat(key); e != nil {
			return cred, errors.New("SSH key 文件不可用")
		}
		command += " -i " + key
	} else {
		sock, ok := os.LookupEnv("SSH_AUTH_SOCK")
		if !ok || sock == "" {
			return cred, errors.New("显式允许的 SSH agent 不可用")
		}
		cred.Env = append(cred.Env, "SSH_AUTH_SOCK="+sock)
	}
	cred.Env = append(cred.Env, "GIT_SSH_COMMAND="+command)
	cred.Close = func() error { return nil }
	return cred, nil
}
