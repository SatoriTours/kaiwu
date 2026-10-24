// Package githubauth resolves service-held GitHub credential references and
// provides a controlled Git credential bridge. Credentials never reach pi,
// check commands, previews or the browser, and never ride in argv or URLs.
package githubauth

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Credential references are a fixed set; unknown references are refused.
const (
	RefAPI   = "server_github_api"
	RefHTTPS = "server_github_https"
	RefSSH   = "server_github_ssh"
)

// Environment variables read by the service itself. They are never forwarded
// to git children, pi, checks or previews.
const (
	envAPIToken   = "KAIWU_GITHUB_API_TOKEN"
	envGitToken   = "KAIWU_GITHUB_GIT_TOKEN"
	envSSHKey     = "KAIWU_GITHUB_SSH_KEY"
	envSSHAgentOK = "KAIWU_GITHUB_SSH_AGENT"
)

// Config sources credentials from the service environment via Lookup.
// Executable optionally overrides how the helper program path is resolved; tests
// use it to point at a built binary, production leaves it nil.
type Config struct {
	Lookup     func(string) (string, bool)
	Executable func() (string, error)
	// CanonicalHost overrides the frozen remote host. It exists only so tests can
	// point the resolver at a loopback server; production leaves it empty and the
	// canonical github.com host is enforced.
	CanonicalHost string
}

// Resolver resolves references at request time. It holds no token strings.
type Resolver struct {
	lookup     func(string) (string, bool)
	executable func() (string, error)
	host       string
}

// GitCredential carries only non-secret plumbing for one remote operation.
// Files are passed as extra files to the git process (the credential pipe).
const (
	// maxArrayFields and maxArrayValues bound the array-valued capability fields
	// git may send on a 401 challenge.
	maxArrayFields = 16
	maxArrayValues = 64
	// maxCredentialBytes bounds the whole credential request, not one line.
	maxCredentialBytes = 64 << 10
)

type GitCredential struct {
	Env   []string
	Files []*os.File
	// Config carries explicit git configuration for this invocation only. It is
	// how the controlled credential helper is installed without touching any
	// repository configuration.
	Config []string
	Close  func() error
}

func New(cfg Config) (*Resolver, error) {
	if cfg.Lookup == nil {
		return nil, errors.New("缺少环境查询")
	}
	return &Resolver{lookup: cfg.Lookup, executable: cfg.Executable, host: cfg.CanonicalHost}, nil
}

// Token resolves an API credential reference to a bearer token.
// canonicalHost is the frozen remote host for this resolver. Production always
// resolves to github.com; only tests inject another host.
func (r *Resolver) canonicalHost() string {
	if r.host != "" {
		return r.host
	}
	return "github.com"
}

func (r *Resolver) Token(ctx context.Context, ref string) (string, error) {
	if ref != RefAPI {
		return "", fmt.Errorf("凭据引用 %q 不能用于 API 查询", ref)
	}
	token, ok := r.lookup(envAPIToken)
	if !ok || token == "" {
		return "", errors.New("服务未配置 GitHub API 凭据")
	}
	return token, nil
}

// knownRefs are the only credential references this resolver accepts.
func knownRefs() []string { return []string{RefAPI, RefHTTPS, RefSSH} }

// gitRef reports whether ref is a Git-capable reference.
func gitRef(ref string) bool { return ref == RefHTTPS || ref == RefSSH }

// EnvAllowlist is the exact set of credential-related variables that may ever
// appear in a git child environment. Token values never appear in it.
func EnvAllowlist() []string {
	return []string{
		"GIT_SSH_COMMAND",
		"SSH_AUTH_SOCK",
		"KAIWU_GITHUB_HELPER",
		"KAIWU_GITHUB_CREDENTIAL_FD",
		"KAIWU_GITHUB_CREDENTIAL_HOST",
	}
}

func (r *Resolver) Git(ctx context.Context, ref, canonicalURL string) (GitCredential, error) {
	cred := GitCredential{}
	if !gitRef(ref) {
		return cred, fmt.Errorf("凭据引用 %q 不能用于 Git 远程", ref)
	}
	if ref == RefSSH {
		return r.gitSSH(canonicalURL)
	}
	return r.gitHTTPS(canonicalURL)
}
