package githubauth

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(lookup func(string) (string, bool)) Config {
	return Config{Lookup: lookup}
}

func envLookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

// TestGitHubCredentialScopes: fixed references only, frozen URLs, and no
// credential value in the environment or argv of any subprocess.
func TestGitHubCredentialScopes(t *testing.T) {
	env := map[string]string{
		"KAIWU_GITHUB_API_TOKEN": "api-token-value",
		"KAIWU_GITHUB_GIT_TOKEN": "git-token-value",
	}
	r, err := New(testConfig(envLookup(env)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("token resolves only the api ref", func(t *testing.T) {
		token, e := r.Token(context.Background(), "server_github_api")
		if e != nil || token != "api-token-value" {
			t.Fatalf("Token: %q %v", token, e)
		}
		if _, e := r.Token(context.Background(), "server_github_https"); e == nil {
			t.Fatal("git token ref must not answer API queries")
		}
		if _, e := r.Token(context.Background(), "unknown"); e == nil {
			t.Fatal("unknown ref must be rejected")
		}
	})

	t.Run("missing token is an error", func(t *testing.T) {
		empty, e := New(testConfig(envLookup(map[string]string{})))
		if e != nil {
			t.Fatalf("New without tokens must still construct: %v", e)
		}
		if _, e := empty.Token(context.Background(), "server_github_api"); e == nil {
			t.Fatal("missing api token must fail")
		}
	})

	t.Run("https credential bridge freezes host and hides the token", func(t *testing.T) {
		cred, e := r.Git(context.Background(), "server_github_https", "https://github.com/owner/repo.git")
		if e != nil {
			t.Fatalf("Git https: %v", e)
		}
		defer cred.Close()
		joined := strings.Join(cred.Env, "\n")
		if strings.Contains(joined, "git-token-value") || strings.Contains(joined, "api-token-value") {
			t.Fatal("credential value leaked into environment")
		}
		hasHelper := false
		hasFD := false
		hasHost := false
		for _, kv := range cred.Env {
			switch {
			case strings.HasPrefix(kv, "GIT_ASKPASS=") || strings.HasPrefix(kv, "KAIWU_GITHUB_HELPER="):
				hasHelper = true
			case strings.HasPrefix(kv, "KAIWU_GITHUB_CREDENTIAL_FD="):
				hasFD = true
			case strings.HasPrefix(kv, "KAIWU_GITHUB_CREDENTIAL_HOST="):
				if kv != "KAIWU_GITHUB_CREDENTIAL_HOST=github.com" {
					t.Fatalf("host frozen to %q", kv)
				}
				hasHost = true
			}
		}
		if !hasHelper || !hasFD || !hasHost {
			t.Fatalf("bridge env incomplete: %v", cred.Env)
		}
		if len(cred.Files) != 1 || cred.Files[0] == nil {
			t.Fatal("credential pipe must be passed as an extra file")
		}
	})

	t.Run("non https or non github canonical url is rejected", func(t *testing.T) {
		for _, bad := range []string{
			"http://github.com/owner/repo.git",
			"git@github.com:owner/repo.git",
			"https://evil.invalid/owner/repo.git",
			"https://user:pass@github.com/owner/repo.git",
			"../../relative",
		} {
			if _, e := r.Git(context.Background(), "server_github_https", bad); e == nil {
				t.Fatalf("url %q must be rejected", bad)
			}
		}
	})

	t.Run("ssh credential uses a fixed key with strict options", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "id_ed25519")
		if e := os.WriteFile(keyPath, []byte("synthetic"), 0600); e != nil {
			t.Fatal(e)
		}
		sshEnv := map[string]string{"KAIWU_GITHUB_SSH_KEY": keyPath}
		sshResolver, e := New(testConfig(envLookup(sshEnv)))
		if e != nil {
			t.Fatalf("New ssh: %v", e)
		}
		cred, e := sshResolver.Git(context.Background(), "server_github_ssh", "git@github.com:owner/repo.git")
		if e != nil {
			t.Fatalf("Git ssh: %v", e)
		}
		defer cred.Close()
		command := ""
		for _, kv := range cred.Env {
			if strings.HasPrefix(kv, "GIT_SSH_COMMAND=") {
				command = strings.TrimPrefix(kv, "GIT_SSH_COMMAND=")
			}
		}
		if command == "" {
			t.Fatalf("no ssh command in %v", cred.Env)
		}
		for _, want := range []string{"-i " + keyPath, "BatchMode=yes", "IdentitiesOnly=yes", "StrictHostKeyChecking=yes"} {
			if !strings.Contains(command, want) {
				t.Fatalf("ssh command %q missing %q", command, want)
			}
		}
	})

	t.Run("ssh agent is not inherited unless explicitly allowed", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
		if _, e := r.Git(context.Background(), "server_github_ssh", "git@github.com:owner/repo.git"); e == nil {
			t.Fatal("ssh agent without key or explicit allowance must fail")
		}
		agentEnv := map[string]string{"KAIWU_GITHUB_SSH_AGENT": "allowed"}
		agentResolver, e := New(testConfig(envLookup(agentEnv)))
		if e != nil {
			t.Fatalf("New agent: %v", e)
		}
		cred, e := agentResolver.Git(context.Background(), "server_github_ssh", "git@github.com:owner/repo.git")
		if e != nil {
			t.Fatalf("Git ssh agent: %v", e)
		}
		defer cred.Close()
		sock := false
		for _, kv := range cred.Env {
			if kv == "SSH_AUTH_SOCK=/tmp/agent.sock" {
				sock = true
			}
		}
		if !sock {
			t.Fatalf("allowed agent must pass the socket: %v", cred.Env)
		}
	})

	t.Run("ssh url must be github com", func(t *testing.T) {
		if _, e := r.Git(context.Background(), "server_github_ssh", "git@evil.invalid:owner/repo.git"); e == nil {
			t.Fatal("non github ssh host must be rejected")
		}
	})
}

// TestCredentialBridge answers only the frozen host, only through the
// inherited pipe, and never reads credentials from argv.
func TestCredentialBridge(t *testing.T) {
	env := map[string]string{"KAIWU_GITHUB_GIT_TOKEN": "git-token-value"}
	r, e := New(testConfig(envLookup(env)))
	if e != nil {
		t.Fatal(e)
	}
	newCred := func() GitCredential {
		cred, e := r.Git(context.Background(), "server_github_https", "https://github.com/owner/repo.git")
		if e != nil {
			t.Fatal(e)
		}
		return cred
	}
	run := func(cred GitCredential, args []string, stdin string) (string, bool, error) {
		env := map[string]string{}
		for _, kv := range cred.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		return credentialOutput(env, func() (io.Reader, error) { return cred.Files[0], nil }, args, strings.NewReader(stdin))
	}

	t.Run("helper answers the frozen host", func(t *testing.T) {
		cred := newCred()
		defer cred.Close()
		out, handled, e := run(cred, []string{"internal-git-credential", "get"}, "url=https://github.com/owner/repo.git\n")
		if e != nil || !handled {
			t.Fatalf("handled: %v %v", handled, e)
		}
		if !strings.Contains(out, "username=x-access-token") || !strings.Contains(out, "password=git-token-value") {
			t.Fatalf("helper output: %q", out)
		}
	})

	t.Run("helper refuses other hosts and operations", func(t *testing.T) {
		cred := newCred()
		defer cred.Close()
		if _, handled, e := run(cred, []string{"internal-git-credential", "get"}, "url=https://evil.invalid/owner/repo.git\n"); e == nil || !handled {
			t.Fatalf("wrong host must be refused: %v %v", handled, e)
		}
		// store/erase are deliberate silent no-ops: real git calls store after a
		// successful authentication and the bridge has no credential store.
		for _, op := range []string{"store", "erase"} {
			out, handled, e := run(cred, []string{"internal-git-credential", op}, "protocol=https\nhost=github.com\npassword=argv-secret\n\n")
			if !handled || e != nil || out != "" {
				t.Fatalf("%s must be a silent no-op: handled=%v err=%v out=%q", op, handled, e, out)
			}
		}
		if _, handled, e := run(cred, []string{"internal-git-credential", "unknown-op"}, ""); !handled || e == nil {
			t.Fatalf("unknown operation must be refused: handled=%v err=%v", handled, e)
		}
	})

	t.Run("non bridge invocation is not handled", func(t *testing.T) {
		cred := newCred()
		defer cred.Close()
		if _, handled, e := run(cred, []string{"serve"}, ""); e != nil || handled {
			t.Fatalf("ordinary subcommand must pass through: %v %v", handled, e)
		}
	})

	t.Run("credentials never come from argv", func(t *testing.T) {
		cred := newCred()
		defer cred.Close()
		out, handled, e := run(cred, []string{"internal-git-credential", "get", "password=argv-secret"}, "url=https://github.com/owner/repo.git\n")
		if e != nil || !handled {
			t.Fatalf("handled: %v %v", handled, e)
		}
		if strings.Contains(out, "argv-secret") {
			t.Fatal("token from argv must be ignored")
		}
	})
}

// TestCredentialEnvironmentSeparation: the resolver exports an exact env
// allowlist so no credential value can ride into pi, checks or previews.
func TestCredentialEnvironmentSeparation(t *testing.T) {
	allow := EnvAllowlist()
	for _, name := range allow {
		if strings.Contains(strings.ToLower(name), "token") && !strings.HasSuffix(name, "_FD") {
			t.Fatalf("allowlist must never carry token values: %q", name)
		}
	}
	// The production git env stays free of KAIWU_GITHUB_* values: they are
	// only consumed by the resolver and the bridge child process.
	for _, name := range []string{"KAIWU_GITHUB_API_TOKEN", "KAIWU_GITHUB_GIT_TOKEN", "KAIWU_GITHUB_SSH_KEY"} {
		if containsExact(allow, name) {
			t.Fatalf("secret env name %q must not be forwarded to git children", name)
		}
	}
}

func containsExact(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
