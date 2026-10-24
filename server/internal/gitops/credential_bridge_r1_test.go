package gitops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
)

// loopbackResolver is the production HTTPS resolver with only the frozen remote
// host replaced for an offline loopback server. It is a test-only injection: the
// production canonical host check stays github.com.
func loopbackResolver(t *testing.T, host, token string) *githubauth.Resolver {
	t.Helper()
	bin := realServerBinary(t)
	r, e := githubauth.New(githubauth.Config{Lookup: func(k string) (string, bool) {
		if k == "KAIWU_GITHUB_GIT_TOKEN" {
			return token, true
		}
		return "", false
	}, Executable: func() (string, error) { return bin, nil }, CanonicalHost: host})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

// real401 runs a real git clone against a loopback HTTPS server that answers 401
// once. Git performs its genuine credential handshake, including the
// capability[]/wwwauth[] arrays, through the controlled helper.
func real401(t *testing.T, token string, withBridge bool) (authOK bool, gitOut string, gitErr error) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); ok && u == "x-access-token" && p == token {
			authOK = true
			// Serve a valid empty upload-pack advertisement so git can finish the
			// whole authenticated flow, including its trailing helper store call.
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = w.Write([]byte("001e# service=git-upload-pack\n00000000"))
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="probe"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	dir := t.TempDir()
	host := strings.TrimPrefix(server.URL, "https://")
	g(t, dir, "init", "--template=")
	url := server.URL + "/o/r.git"
	var config []string
	var env []string
	var files []*os.File
	if withBridge {
		cred, e := loopbackResolver(t, host, token).Git(context.Background(), githubauth.RefHTTPS, "https://"+host+"/o/r.git")
		if e != nil {
			t.Fatalf("resolve: %v", e)
		}
		defer func() { _ = cred.Close() }()
		config, env, files = cred.Config, cred.Env, cred.Files
	} else {
		// Allow the self-signed loopback certificate only; no credential config.
		env = append(env, "GIT_SSL_NO_VERIFY=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", bridgeArgs(dir, config, "clone", "--no-checkout", url, filepath.Join(dir, "clone"))...)
	cmd.Env = append(append(Env(), env...), "GIT_SSL_NO_VERIFY=1")
	cmd.ExtraFiles = files
	out, e := cmd.CombinedOutput()
	return authOK, string(out), e
}

// C1: git really sends legitimate repeated array fields on a 401. Rejecting them
// as "duplicate" breaks the first authentication of any private repository.
func TestCredentialBridgeAcceptsGit401Arrays(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	if _, e := loopbackResolver(t, "127.0.0.1:1", token).Git(context.Background(), githubauth.RefHTTPS, "https://127.0.0.1:1/o/r.git"); e != nil {
		t.Fatalf("loopback resolve: %v", e)
	}
	ok, out, err := real401(t, token, true)
	if !ok || strings.Contains(out, token) {
		t.Fatalf("C1 FINDING: real 401 handshake did not authenticate (auth=%v): %s", ok, out)
	}
	if err != nil {
		t.Fatalf("C1 FINDING: authenticated flow did not finish cleanly: %v %s", err, out)
	}
}

// A mixed request that claims github.com in url= but another host in host= must
// never be answered with a credential.
func TestCredentialBridgeRejectsMixedHostFields(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	bin := realServerBinary(t)
	r, e := githubauth.New(githubauth.Config{Lookup: func(k string) (string, bool) {
		if k == "KAIWU_GITHUB_GIT_TOKEN" {
			return token, true
		}
		return "", false
	}, Executable: func() (string, error) { return bin, nil }})
	if e != nil {
		t.Fatal(e)
	}
	cred, e := r.Git(context.Background(), githubauth.RefHTTPS, "https://github.com/owner/repo.git")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cred.Close() }()
	for _, stdin := range []string{
		"url=https://github.com/o/r.git\nhost=evil.example\n\n",
		"url=https://github.com/o/r.git\nprotocol=http\n\n",
		"url=https://github.com/o/r.git\npath=other/repo.git\n\n",
		"protocol=https\nhost=github.com\nurl=https://github.com/o/r.git\n\n",
	} {
		out, e := fill(t, t.TempDir(), cred, stdin)
		if e == nil || strings.Contains(out, token) {
			t.Fatalf("C2 FINDING: mixed request answered: %q %v", stdin, e)
		}
	}
}

// The controlled pipe is single-use: a second get must fail closed quickly
// rather than block while the parent waits for git to exit.
func TestCredentialBridgeSecondGetFailsClosed(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	cred := bridgeCredential(t, token)
	defer func() { _ = cred.Close() }()
	dir := t.TempDir()
	// Both invocations share the very same pipe; nothing is closed in between, so
	// the probe shows the pipe itself is single-use rather than exec failing.
	run := func() (string, error) {
		cmd := exec.Command("git", bridgeArgs(dir, cred.Config, "credential", "fill")...)
		cmd.Env = append(Env(), cred.Env...)
		cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
		cmd.ExtraFiles = cred.Files
		out, e := cmd.CombinedOutput()
		return string(out), e
	}
	if out, e := run(); e != nil || !strings.Contains(out, "password=") {
		t.Fatalf("first get: %v %q", e, out)
	}
	done := make(chan struct{})
	var out string
	var err error
	go func() { out, err = run(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("FD FINDING: second get blocked instead of failing closed")
	}
	if err == nil || strings.Contains(out, token) {
		t.Fatalf("second get served a credential: %q %v", out, err)
	}
}

var _ = fmt.Sprintf

// Git calls store/erase after a real authentication. They must be harmless
// no-ops that never read the token pipe and never write a credential anywhere.
func TestCredentialBridgeStoreAndEraseAreNoOps(t *testing.T) {
	token := "fake-token-not-a-real-credential"
	cred := bridgeCredential(t, token)
	defer func() { _ = cred.Close() }()
	// "git credential approve/reject" is what really drives the helper's
	// store/erase operations; the helper protocol has no separate git subcommand
	// for them.
	for _, op := range []string{"approve", "reject"} {
		cmd := exec.Command("git", bridgeArgs(t.TempDir(), cred.Config, "credential", op)...)
		cmd.Env = append(Env(), cred.Env...)
		cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\nusername=x-access-token\npassword=" + token + "\n\n")
		cmd.ExtraFiles = cred.Files
		out, e := cmd.CombinedOutput()
		if e != nil || len(out) != 0 || strings.Contains(string(out), token) {
			t.Fatalf("%s was not a silent no-op: %v %q", op, e, out)
		}
	}
	// The token must still be available: a no-op never consumes the pipe.
	if out, e := fill(t, t.TempDir(), cred, "protocol=https\nhost=github.com\n\n"); e != nil || !strings.Contains(out, "password="+token) {
		t.Fatalf("pipe was consumed by store/erase: %v", e)
	}
}
