//go:build !windows

package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The shim forwards local configuration reads to real Git, but records every
// network command without contacting a remote. These tests exercise the actual
// production child environment and entry point, rather than an injected transport.
func remoteBoundaryShim(t *testing.T) (calls, leaked string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls, leaked = filepath.Join(bin, "calls"), filepath.Join(bin, "leaked")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\n" +
		"for arg do\n" +
		"  case \"$arg\" in\n" +
		"    ls-remote|fetch|push)\n" +
		"      printf '%s\\n' \"$arg\" >> " + quote(calls) + "\n" +
		"      if [ \"${OTHER_VENDOR_API_TOKEN+x}\" = x ]; then printf present > " + quote(leaked) + "; fi\n" +
		"      printf '%s\\t%s\\n' '" + testSHA5 + "' 'refs/heads/main'\n" +
		"      exit 0;;\n" +
		"  esac\n" +
		"done\nexec " + quote(git) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	return calls, leaked
}

func deliveryBoundaryTarget() RemoteTarget {
	return RemoteTarget{RepositoryID: 123, URL: "https://github.com/owner/repo.git", MainRef: "main", TaskRef: "refs/heads/kaiwu/task-0123456789abcdef0123456789abcdef"}
}

func TestProductionRemoteWithholdsUnrelatedServiceCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	repo := initGitRepo(t, t.TempDir())
	calls, leaked := remoteBoundaryShim(t)
	t.Setenv("OTHER_VENDOR_API_TOKEN", "synthetic-not-a-real-credential")
	sha, err := NewRemoteRunner(RemoteConfig{}).Observe(ctx, repo, deliveryBoundaryTarget(), "refs/heads/main", "", nil)
	if err != nil || sha != testSHA5 {
		t.Fatalf("production command did not run: sha=%q err=%v", sha, err)
	}
	if _, err := os.Stat(calls); err != nil {
		t.Fatalf("production child was not observed: %v", err)
	}
	if _, err := os.Stat(leaked); !os.IsNotExist(err) {
		t.Fatal("an unrelated service credential reached the production Git child")
	}
}

type boundaryLease struct {
	attempted, started, empty int
}

func (*boundaryLease) File() *os.File      { return nil }
func (l *boundaryLease) Attempt() error    { l.attempted++; return nil }
func (l *boundaryLease) Started(int) error { l.started++; return nil }
func (l *boundaryLease) NoProcess()        { l.empty++ }

func TestProductionRemoteRevalidatesFrozenConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		config []string
		url    string
	}{
		{name: "remote changed", config: []string{"remote.origin.url", "https://github.com/other/repo.git"}},
		{name: "extra fetch URL", config: []string{"--add", "remote.origin.url", "https://github.com/other/repo.git"}},
		{name: "multiline URL cannot forge a remote", config: []string{"remote.origin.url", "https://unapproved.invalid/repo.git\nremote.forged.url https://github.com/owner/repo.git"}},
		{name: "different push URL", config: []string{"remote.origin.pushurl", "https://github.com/other/repo.git"}},
		{name: "fetch rewrite", config: []string{"url.file:///unapproved/.insteadOf", "https://github.com/"}},
		{name: "push rewrite", config: []string{"url.file:///unapproved/.pushInsteadOf", "https://github.com/"}},
		{name: "remote helper", config: []string{"remote.origin.vcs", "unapproved"}},
		{name: "credential helper", config: []string{"credential.helper", "!exit 1"}},
		{name: "unapproved host", config: []string{"remote.origin.url", "https://unapproved.invalid/owner/repo.git"}, url: "https://unapproved.invalid/owner/repo.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitRepo(t, t.TempDir())
			if _, err := gitCfg(t, repo, tc.config...); err != nil {
				t.Fatal(err)
			}
			calls, _ := remoteBoundaryShim(t)
			target := deliveryBoundaryTarget()
			if tc.url != "" {
				target.URL = tc.url
			}
			lease := &boundaryLease{}
			_, err := NewRemoteRunner(RemoteConfig{}).Observe(context.Background(), repo, target, "refs/heads/main", "", lease)
			if err == nil {
				t.Error("production entry accepted unapproved remote configuration")
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Error("remote command ran before the configuration was rejected")
			}
			if lease.started != 0 || lease.empty != 1 {
				t.Errorf("rejection must prove no remote child started: %+v", lease)
			}
		})
	}
}

func TestDeliveryRemoteOperationsStayWithinFrozenRefs(t *testing.T) {
	for _, operation := range []string{"observe", "fetch", "push"} {
		for _, ref := range []string{"refs/heads/other", "refs/heads/kaiwu/task-other", "refs/heads/main/child"} {
			t.Run(operation+"/"+ref, func(t *testing.T) {
				called := false
				runner := NewRemoteRunner(RemoteConfig{Transport: RemoteTransportFunc(func(context.Context, string, RemoteTarget, []string, string, ProcessLease) (string, error) {
					called = true
					return "", nil
				})})
				target, lease := deliveryBoundaryTarget(), &boundaryLease{}
				var err error
				switch operation {
				case "observe":
					_, err = runner.Observe(context.Background(), t.TempDir(), target, ref, "", lease)
				case "fetch":
					_, err = runner.Fetch(context.Background(), t.TempDir(), target, ref, "", lease)
				case "push":
					_, err = runner.Push(context.Background(), t.TempDir(), target, testSHA5, ref, "", lease)
				}
				if err == nil || called || lease.attempted != 0 {
					t.Fatalf("out-of-scope ref reached execution: err=%v called=%v lease=%+v", err, called, lease)
				}
			})
		}
	}
}

type httpOverrideCase struct {
	name, key, value, include string
}

func httpOverrideCases() []httpOverrideCase {
	return []httpOverrideCase{
		{name: "TLS verification", key: "http.sslVerify", value: "false"},
		{name: "URL scoped mixed case", key: "hTtP.https://github.com/.sSlVeRiFy", value: "false"},
		{name: "CA file", key: "http.sslCAInfo", value: "/synthetic/attacker-ca.pem"},
		{name: "CA directory", key: "http.sslCAPath", value: "/synthetic/attacker-ca"},
		{name: "proxy", key: "http.proxy", value: "http://127.0.0.1:1"},
		{name: "proxy TLS", key: "http.proxySSLVerify", value: "false"},
		{name: "redirects", key: "http.followRedirects", value: "true"},
		{name: "extra header", key: "http.extraHeader", value: "X-Synthetic: untrusted"},
		{name: "include", key: "http.https://github.com/.sslCAInfo", value: "/synthetic/attacker-ca.pem", include: "include.path"},
		{name: "conditional include", key: "http.proxy", value: "http://127.0.0.1:1", include: "conditional"},
	}
}

func configureHTTPOverride(t *testing.T, repo string, tc httpOverrideCase) {
	t.Helper()
	if tc.include == "" {
		if _, err := gitCfg(t, repo, tc.key, tc.value); err != nil {
			t.Fatal(err)
		}
	} else {
		extra := filepath.Join(t.TempDir(), "http.conf")
		if _, err := gitCfg(t, repo, "--file", extra, tc.key, tc.value); err != nil {
			t.Fatal(err)
		}
		key := tc.include
		if key == "conditional" {
			key = "includeIf.gitdir:" + filepath.Join(repo, ".git") + ".path"
		}
		if _, err := gitCfg(t, repo, key, extra); err != nil {
			t.Fatal(err)
		}
	}
	// Ensure conditional/include fixtures are effective rather than silently
	// passing because Git did not load the attacker's configuration.
	if out, err := gitCfg(t, repo, "--includes", "--get-regexp", `^http\.`); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("HTTP override fixture was not active: %v", err)
	}
}

func TestProductionRemoteRejectsHTTPTransportOverrides(t *testing.T) {
	for _, tc := range httpOverrideCases() {
		for _, manual := range []bool{false, true} {
			name := "delivery/"
			if manual {
				name = "manual/"
			}
			t.Run(name+tc.name, func(t *testing.T) {
				repo := initGitRepo(t, t.TempDir())
				configureHTTPOverride(t, repo, tc)
				calls, _ := remoteBoundaryShim(t)
				target, lease := deliveryBoundaryTarget(), &boundaryLease{}
				if manual {
					target.RepositoryID = 0
				}
				_, err := NewRemoteRunner(RemoteConfig{}).Observe(context.Background(), repo, target, "refs/heads/main", "", lease)
				if err == nil {
					t.Error("repository HTTP transport override was accepted")
				}
				if _, err := os.Stat(calls); !os.IsNotExist(err) {
					t.Error("remote child ran with untrusted HTTP configuration")
				}
				if lease.started != 0 || lease.empty != 1 {
					t.Errorf("no-process proof missing: %+v", lease)
				}
			})
		}
	}
}

func TestManualReviewRejectsHTTPTransportOverrides(t *testing.T) {
	for _, tc := range httpOverrideCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := reviewFixture(t)
			if _, err := ResolveManualRemote(context.Background(), f.project); err != nil {
				t.Fatalf("valid manual fixture: %v", err)
			}
			configureHTTPOverride(t, f.project.Path, tc)
			if _, err := ResolveManualRemote(context.Background(), f.project); err == nil {
				t.Fatal("manual review accepted repository HTTP transport override")
			}
		})
	}
}

func TestGitCredentialFillCannotExecuteRepositoryAskpass(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	bin := t.TempDir()
	marker, script := filepath.Join(bin, "askpass-ran"), filepath.Join(bin, "askpass")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\nprintf synthetic\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCfg(t, repo, "core.askPass", script); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", Args(repo, "credential", "fill")...)
	cmd.Env = Env()
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	_, err := cmd.CombinedOutput()
	if _, markerErr := os.Stat(marker); !os.IsNotExist(markerErr) {
		t.Fatal("repository askpass executable ran")
	}
	if err == nil {
		t.Fatal("credential fill unexpectedly obtained credentials")
	}
}
