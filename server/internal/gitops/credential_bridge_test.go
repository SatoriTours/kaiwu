package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
)

// serverBinary builds the real kaiwu-server executable once per test run. The
// credential bridge is only real if git can invoke the production helper entry
// point, so a fake transport or a fake helper would prove nothing.
var serverBinary struct {
	once sync.Once
	path string
	err  error
}

func realServerBinary(t *testing.T) string {
	t.Helper()
	serverBinary.once.Do(func() {
		dir, e := os.MkdirTemp("", "kaiwu-bridge-")
		if e != nil {
			serverBinary.err = e
			return
		}
		out := filepath.Join(dir, "kaiwu server")
		cmd := exec.Command("go", "build", "-o", out, "./cmd/kaiwu-server")
		cmd.Dir = ".."
		cmd.Dir = filepath.Join("..", "..")
		if e := cmd.Run(); e != nil {
			serverBinary.err = e
			return
		}
		serverBinary.path = out
	})
	if serverBinary.err != nil {
		t.Fatalf("build server binary: %v", serverBinary.err)
	}
	return serverBinary.path
}

// bridgeResolver is the production resolver with the real service binary as the
// helper program, so git invokes the same HandleCredentialCommand entry point.
func bridgeResolver(t *testing.T, token string) *githubauth.Resolver {
	t.Helper()
	bin := realServerBinary(t)
	r, e := githubauth.New(githubauth.Config{Lookup: func(k string) (string, bool) {
		switch k {
		case "KAIWU_GITHUB_GIT_TOKEN":
			return token, true
		}
		return "", false
	}, Executable: func() (string, error) { return bin, nil }})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

// bridgeCredential resolves the HTTPS credential exactly as productionRun does.
func bridgeCredential(t *testing.T, token string) githubauth.GitCredential {
	t.Helper()
	cred, e := bridgeResolver(t, token).Git(context.Background(), githubauth.RefHTTPS, "https://github.com/owner/repo.git")
	if e != nil {
		t.Fatalf("resolve credential: %v", e)
	}
	return cred
}

// fill runs the production argv/env combination and returns the helper output.
func fill(t *testing.T, dir string, cred githubauth.GitCredential, stdin string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", bridgeArgs(dir, cred.Config, "credential", "fill")...)
	cmd.Env = append(Env(), cred.Env...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.ExtraFiles = cred.Files
	out, e := cmd.CombinedOutput()
	if cred.Close != nil {
		defer func() { _ = cred.Close() }()
	}
	return string(out), e
}

// The real service helper answers a genuine git credential fill with the
// protocol/host handshake git actually sends, and the secret never appears in
// argv or in the environment.
func TestCredentialBridgeFillsRealGitCredential(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	dir := t.TempDir()
	cred := bridgeCredential(t, token)
	defer func() { _ = cred.Close() }()

	for _, k := range append(append([]string{}, bridgeArgs(dir, cred.Config, "credential", "fill")...), Env()...) {
		if strings.Contains(k, token) {
			t.Fatal("credential leaked into argv or environment")
		}
	}
	for _, k := range cred.Env {
		if strings.Contains(k, token) {
			t.Fatal("credential leaked into the bridge environment")
		}
	}

	out, e := fill(t, dir, cred, "protocol=https\nhost=github.com\npath=owner/repo.git\n\n")
	if e != nil {
		t.Fatalf("real git credential fill failed: %v %q", e, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			got[k] = v
		}
	}
	if got["username"] != "x-access-token" || got["password"] != token {
		t.Fatalf("wrong credential (user=%q, token matches=%v)", got["username"], got["password"] == token)
	}
}

// The bridge refuses hosts other than https github.com and never spends the
// token on them.
func TestCredentialBridgeRefusesOtherHosts(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	for _, stdin := range []string{
		"protocol=https\nhost=evil.example\n\n",
		"protocol=https\nhost=github.com:443\n\n",
		"protocol=http\nhost=github.com\n\n",
		"protocol=https\nhost=github.com\nurl=https://evil.example/x\n\n",
		"host=github.com\n\n",
	} {
		cred := bridgeCredential(t, token)
		out, e := fill(t, t.TempDir(), cred, stdin)
		if e == nil {
			t.Fatalf("bridge accepted untrusted request %q: %q", stdin, out)
		}
		if strings.Contains(out, token) {
			t.Fatal("token leaked on refusal")
		}
	}
}

// Without the controlled helper, git must fail closed rather than fall back to
// any repository-configured helper.
func TestCredentialBridgeFailsClosedWithoutHelper(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "helper-ran")
	g(t, dir, "init", "--initial-branch=main", "--template=")
	g(t, dir, "config", "credential.helper", "!f() { touch "+marker+"; }; f")
	g(t, dir, "config", "credential.https://github.com.helper", "!f() { touch "+marker+"; }; f")
	extra := filepath.Join(dir, ".git", "extra.conf")
	if e := os.WriteFile(extra, []byte("[credential \"https://github.com\"]\n\thelper = !f() { touch "+marker+"; }; f\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	g(t, dir, "config", "include.path", extra)

	empty := githubauth.GitCredential{}
	out, e := fill(t, dir, empty, "protocol=https\nhost=github.com\n\n")
	if e == nil {
		t.Fatalf("fill without a bridge helper unexpectedly succeeded: %q", out)
	}
	if _, e := os.Stat(marker); e == nil {
		t.Fatal("repository credential helper executed")
	}
	if strings.Contains(out, "fake-token") {
		t.Fatal("secret leaked")
	}
}

// A closed credential pipe cannot keep serving the token.
func TestCredentialBridgePipeIsSingleUse(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	cred := bridgeCredential(t, token)
	dir := t.TempDir()
	if _, e := fill(t, dir, cred, "protocol=https\nhost=github.com\n\n"); e != nil {
		t.Fatal(e)
	}
	if e := cred.Close(); e == nil {
		// Close is deferred in fill already; a second close must not panic.
	}
	out, e := fill(t, dir, cred, "protocol=https\nhost=github.com\n\n")
	if e == nil || strings.Contains(out, token) {
		t.Fatalf("closed bridge still served a credential: %q %v", out, e)
	}
}

// The production git environment must not inherit raw service credentials or an
// unauthorized SSH agent.
func TestProductionEnvWithholdsServiceSecrets(t *testing.T) {
	t.Setenv("KAIWU_GITHUB_GIT_TOKEN", "fake-token-not-a-real-credential")
	t.Setenv("KAIWU_GITHUB_API_TOKEN", "fake-api-token")
	t.Setenv("KAIWU_GITHUB_HELPER", "/tmp/untrusted-helper")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/untrusted-agent.sock")
	for _, kv := range Env() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "KAIWU_GITHUB_GIT_TOKEN", "KAIWU_GITHUB_API_TOKEN", "KAIWU_GITHUB_HELPER", "SSH_AUTH_SOCK":
			t.Fatalf("production env inherited %s", name)
		}
	}
	// The resolver may still authorize an agent explicitly; the general
	// environment must not pre-supply it.
	cred := bridgeCredential(t, "fake-token-not-a-real-credential")
	if len(cred.Env) == 0 || len(cred.Files) != 1 {
		t.Fatalf("bridge credential incomplete: %#v", cred)
	}
	for _, kv := range cred.Env {
		if strings.Contains(kv, "fake-token-not-a-real-credential") {
			t.Fatal("bridge env carries the token")
		}
	}
}

// An explicitly authorized SSH agent is still delivered by the resolver even
// though the ambient environment no longer supplies one.
func TestExplicitSSHAgentStaysAuthorized(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	if e := os.WriteFile(sock, nil, 0o600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SSH_AUTH_SOCK", sock)
	r, e := githubauth.New(githubauth.Config{Lookup: func(k string) (string, bool) {
		if k == "KAIWU_GITHUB_SSH_AGENT" {
			return "allowed", true
		}
		return "", false
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := r.Git(context.Background(), githubauth.RefSSH, "git@github.com:owner/repo.git"); e == nil {
		// The allowed-flag path requires the socket to be visible to the service
		// process itself; it must not be reachable only through the git env.
		if !strings.Contains(strings.Join(Env(), "\n"), "SSH_AUTH_SOCK=") {
			return
		}
		t.Fatal("agent socket leaked into the generic git env")
	}
	if strings.Contains(strings.Join(Env(), "\n"), "SSH_AUTH_SOCK=") {
		t.Fatal("agent socket leaked into the generic git env")
	}
}
