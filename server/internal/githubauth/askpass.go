package githubauth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
)

// The HTTPS credential bridge: the server process itself acts as the git
// credential helper subcommand. Git invokes
//
//	<kaiwu-server> internal-git-credential get
//
// with `url=...` on stdin. The helper answers only the frozen host and reads
// the token from an inherited pipe file descriptor, never from argv.
const (
	helperSubcommand = "internal-git-credential"
	envHelperFD      = "KAIWU_GITHUB_CREDENTIAL_FD"
	envHelperHost    = "KAIWU_GITHUB_CREDENTIAL_HOST"
	envHelperBin     = "KAIWU_GITHUB_HELPER"
)

func (r *Resolver) gitHTTPS(canonicalURL string) (GitCredential, error) {
	cred := GitCredential{}
	u, e := url.Parse(canonicalURL)
	if e != nil || u.Scheme != "https" || u.Host != r.canonicalHost() || u.User != nil {
		return cred, fmt.Errorf("远程地址 %q 不是受支持的 github.com HTTPS 地址", canonicalURL)
	}
	token, ok := r.lookup(envGitToken)
	if !ok || token == "" {
		return cred, errors.New("服务未配置 GitHub Git 凭据")
	}
	readEnd, writeEnd, e := os.Pipe()
	if e != nil {
		return cred, e
	}
	self, e := r.helperProgram()
	if e != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return cred, e
	}
	// The git child inherits the read end as fd 3. The token is written once and
	// the write end is closed immediately, so the pipe is single-use: a second
	// helper invocation reads EOF and fails closed instead of blocking while the
	// parent waits for git to exit.
	go func() {
		_, _ = io.WriteString(writeEnd, token+"\n")
		_ = writeEnd.Close()
	}()
	cred.Files = []*os.File{readEnd}
	cred.Env = []string{
		envHelperBin + "=" + self,
		envHelperFD + "=3",
		envHelperHost + "=" + r.canonicalHost(),
	}
	// The controlled helper is supplied as explicit git configuration, never
	// through repository config: an empty helper value first resets git's
	// helper list, then the service inserts itself as the only helper.
	// Git appends the operation name to a "!"-helper command, so the fixed
	// subcommand word is part of the configuration value and the only value the
	// child can add is "get".
	cred.Config = []string{"credential.helper=", "credential.helper=!" + quoteHelper(self) + " " + helperSubcommand}
	var closeOnce sync.Once
	var closeErr error
	cred.Close = func() error {
		closeOnce.Do(func() {
			// Writing already closed the write end; closing again only reports
			// the harmless "already closed" case.
			if e := writeEnd.Close(); e != nil && !errors.Is(e, os.ErrClosed) {
				closeErr = e
			}
			if e := readEnd.Close(); e != nil && !errors.Is(e, os.ErrClosed) {
				closeErr = errors.Join(closeErr, e)
			}
		})
		return closeErr
	}
	return cred, nil
}

// HandleCredentialCommand implements the hidden credential helper subcommand
// for production: environment, stdin and fd 3 come from the process itself.
// It returns handled=false for any other argv so the server can start
// normally. It never reads credentials from argv.
func HandleCredentialCommand(args []string) (handled bool, err error) {
	if len(args) == 0 || args[0] != helperSubcommand {
		return false, nil
	}
	env := map[string]string{envHelperHost: os.Getenv(envHelperHost), envHelperFD: os.Getenv(envHelperFD)}
	token := func() (io.Reader, error) { return os.NewFile(3, "credential-pipe"), nil }
	out, handled, e := credentialOutput(env, token, args, os.Stdin)
	if e != nil {
		return handled, e
	}
	if handled {
		_, _ = io.WriteString(os.Stdout, out)
	}
	return true, nil
}

// credentialOutput is the injectable core used by tests and the production
// wrapper. env carries only non-secret plumbing values.
func credentialOutput(env map[string]string, token func() (io.Reader, error), args []string, stdin io.Reader) (string, bool, error) {
	if len(args) < 2 || args[0] != helperSubcommand {
		return "", false, nil
	}
	switch args[1] {
	case "get":
	case "store", "erase":
		// Git calls store after a successful authentication. The bridge has no
		// credential store: it must not read the token, write anything or emit
		// output, so the call is a deliberate no-op.
		return "", true, nil
	default:
		return "", true, fmt.Errorf("credential helper 不支持操作 %q", args[1])
	}
	// Extra argv is deliberately ignored: credentials never come from argv.
	host := env[envHelperHost]
	fd := env[envHelperFD]
	if host == "" || fd != "3" {
		return "", true, errors.New("credential 桥未通过受控父进程调用")
	}
	// Git sends either a full url= field or the protocol/host[/path] fields.
	// Both are parsed strictly against the frozen host; nothing is read from the
	// token pipe unless the request is fully trusted.
	fields := map[string]string{}
	// Git sends legitimate repeated keys for array-valued capability fields on a
	// 401 (capability[]=authtype, wwwauth[]=..., state[]=...). They carry no
	// authority and are never used for host decisions, so they are collected
	// separately; every scalar stays single-valued.
	arrays := map[string]int{}
	total := &countingReader{}
	// The scanner bound only limits one line, so a request can still send many
	// legal short lines. The total request size is therefore bounded separately
	// and enforced before any field is trusted, let alone answered.
	reader := io.LimitReader(io.TeeReader(stdin, total), maxCredentialBytes+1)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	for scanner.Scan() {
		line := scanner.Text()
		if total.n > maxCredentialBytes {
			return "", true, errors.New("credential 请求过长")
		}
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", true, errors.New("credential 请求格式无效")
		}
		if strings.HasSuffix(key, "[]") {
			arrays[key]++
			if len(arrays) > maxArrayFields || arrays[key] > maxArrayValues {
				return "", true, errors.New("credential 请求数组字段过多")
			}
			continue
		}
		if _, dup := fields[key]; dup {
			return "", true, errors.New("credential 请求字段重复")
		}
		fields[key] = value
	}
	if e := scanner.Err(); e != nil || total.n > maxCredentialBytes {
		return "", true, errors.New("credential 请求过长")
	}
	requested, hasURL := fields["url"]
	if hasURL {
		// A url= request must not carry any other authority context that could
		// disagree with it: the credential is only ever answered for one fully
		// consistent request.
		for _, k := range []string{"protocol", "host", "path"} {
			if _, ok := fields[k]; ok {
				return "", true, errors.New("credential 请求字段冲突")
			}
		}
		if u, e := url.Parse(requested); e != nil || u.User != nil || u.Host != host || u.Scheme != "https" {
			return "", true, errors.New("credential 请求主机与冻结主机不一致")
		}
	} else {
		if fields["protocol"] != "https" || fields["host"] != host {
			return "", true, errors.New("credential 请求主机与冻结主机不一致")
		}
		if path := fields["path"]; path != "" {
			if u, e := url.Parse("https://" + host + "/" + path); e != nil || u.Host != host || u.User != nil {
				return "", true, errors.New("credential 请求路径无效")
			}
		}
	}
	reader, e := token()
	if e != nil {
		return "", true, e
	}
	line, e := bufio.NewReader(reader).ReadString('\n')
	if e != nil && e != io.EOF {
		return "", true, e
	}
	tokenValue := strings.TrimRight(line, "\r\n")
	if tokenValue == "" {
		return "", true, errors.New("credential 描述符内容为空")
	}
	return "username=x-access-token\npassword=" + tokenValue + "\n", true, nil
}

// helperProgram returns the credential helper program path: the running service
// binary in production, an explicit test binary when configured.
// countingReader records how much of the request body was consumed.
type countingReader struct{ n int64 }

func (c *countingReader) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func (r *Resolver) helperProgram() (string, error) {
	if r.executable != nil {
		return r.executable()
	}
	return os.Executable()
}

// quoteHelper renders an absolute path for a git configuration helper value.
// Git runs the value through a shell, so the path is single quoted with every
// embedded single quote escaped; no metacharacter can break out.
func quoteHelper(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
