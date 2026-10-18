package githubauth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
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
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
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
	self, e := os.Executable()
	if e != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return cred, e
	}
	// The parent keeps the write end; the git child inherits the read end as
	// fd 3. Closing the write end signals EOF to the bridge.
	go func() {
		_, _ = io.WriteString(writeEnd, token+"\n")
	}()
	cred.Files = []*os.File{readEnd}
	cred.Env = []string{
		envHelperBin + "=" + self,
		envHelperFD + "=3",
		envHelperHost + "=github.com",
	}
	cred.Close = func() error {
		e := writeEnd.Close()
		return errors.Join(e, readEnd.Close())
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
	default:
		return "", true, fmt.Errorf("credential helper 不支持操作 %q", args[1])
	}
	// Extra argv is deliberately ignored: credentials never come from argv.
	host := env[envHelperHost]
	fd := env[envHelperFD]
	if host == "" || fd != "3" {
		return "", true, errors.New("credential 桥未通过受控父进程调用")
	}
	requested := ""
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "url=") {
			requested = strings.TrimPrefix(line, "url=")
		}
	}
	if requested == "" {
		return "", true, errors.New("credential 请求缺少 url")
	}
	u, e := url.Parse(requested)
	if e != nil || u.Host != host || (u.Scheme != "https" && u.Scheme != "") {
		return "", true, errors.New("credential 请求主机与冻结主机不一致")
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
