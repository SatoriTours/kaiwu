//go:build !windows

package githubauth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sshCommandEnv(cred GitCredential) string {
	for _, kv := range cred.Env {
		if s, ok := strings.CutPrefix(kv, "GIT_SSH_COMMAND="); ok {
			return s
		}
	}
	return ""
}

func TestSSHCredentialCanonicalURLsAndIdentitySources(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key with space")
	if err := os.WriteFile(key, []byte("synthetic-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo.git"} {
		for _, agent := range []bool{false, true} {
			t.Run(raw+"/agent="+map[bool]string{false: "false", true: "true"}[agent], func(t *testing.T) {
				env := map[string]string{envSSHKey: key}
				if agent {
					env = map[string]string{envSSHAgentOK: "allowed", "SSH_AUTH_SOCK": "/synthetic/authorized-agent.sock"}
				}
				r, _ := New(Config{Lookup: envLookup(env)})
				cred, err := r.Git(context.Background(), RefSSH, raw)
				if err != nil {
					t.Fatal(err)
				}
				defer cred.Close()
				command := sshCommandEnv(cred)
				// Check the isolation flag before invoking real OpenSSH: the RED
				// version must never read the user's actual ~/.ssh/config.
				if !strings.Contains(command, "-F none") {
					t.Fatal("SSH still loads personal/system configuration")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "sh", "-c", command+" -G git@github.com")
				cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, cred.Env...)
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("OpenSSH config evaluation: %v", err)
				}
				cfg := map[string]string{}
				for _, line := range strings.Split(string(out), "\n") {
					k, v, _ := strings.Cut(line, " ")
					cfg[k] = v
				}
				for k, want := range map[string]string{"hostname": "github.com", "user": "git", "batchmode": "yes", "stricthostkeychecking": "true", "forwardagent": "no"} {
					if cfg[k] != want {
						t.Errorf("%s=%q, want %q", k, cfg[k], want)
					}
				}
				if agent {
					if cfg["identitiesonly"] != "no" || cfg["identityfile"] != "none" {
						t.Errorf("agent mode must use authorized agent without default key files: identitiesonly=%q identityfile=%q", cfg["identitiesonly"], cfg["identityfile"])
					}
					if !strings.Contains(strings.Join(cred.Env, "\n"), "SSH_AUTH_SOCK=/synthetic/authorized-agent.sock") {
						t.Fatal("authorized service agent not forwarded")
					}
				} else if cfg["identitiesonly"] != "yes" || cfg["identityagent"] != "none" || cfg["identityfile"] != key {
					t.Errorf("key mode did not restrict identities: identitiesonly=%q identityagent=%q identityfile=%q", cfg["identitiesonly"], cfg["identityagent"], cfg["identityfile"])
				}
			})
		}
	}
}

func TestSSHKeyPathIsOneLiteralShellArgument(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key ' $(touch injected)")
	if err := os.WriteFile(key, []byte("synthetic-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "argv")
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nprintf '%s\\000' \"$@\" > '"+argsFile+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	r, _ := New(Config{Lookup: envLookup(map[string]string{envSSHKey: key})})
	cred, err := r.Git(context.Background(), RefSSH, "git@github.com:owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	defer cred.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", sshCommandEnv(cred)+" git@github.com")
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}, cred.Env...)
	if err := cmd.Run(); err != nil {
		t.Fatalf("SSH command did not preserve literal path: %v", err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(data), "\x00")
	found := false
	for i, arg := range args {
		if arg == "-i" && i+1 < len(args) {
			found = args[i+1] == key
		}
	}
	if !found {
		t.Fatal("identity path was split or expanded")
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("key path executed a shell command")
	}
}

func TestSSHRejectsForeignHostsAndUnapprovedAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/ambient/not-authorized.sock")
	r, _ := New(Config{Lookup: envLookup(map[string]string{envSSHAgentOK: "allowed"})})
	if _, err := r.Git(context.Background(), RefSSH, "git@github.com:owner/repo.git"); err == nil {
		t.Fatal("ambient socket bypassed service credential lookup")
	}
	r, _ = New(Config{Lookup: envLookup(map[string]string{envSSHAgentOK: "allowed", "SSH_AUTH_SOCK": "/synthetic/authorized.sock"})})
	if cred, err := r.Git(context.Background(), RefSSH, "ssh://git@github.com/owner/repo.git"); err != nil {
		t.Fatal("valid credentials control:", err)
	} else {
		cred.Close()
	}
	for _, raw := range []string{"git@github.com:../other/repo.git", "git@github.com:owner/repo.git;command", "ssh://git:password@github.com/owner/repo.git", "ssh://other@github.com/owner/repo.git", "ssh://git@evil.invalid/owner/repo.git", "ssh://git@github.com:2222/owner/repo.git"} {
		if _, err := r.Git(context.Background(), RefSSH, raw); err == nil {
			t.Errorf("unapproved URL accepted: %q", raw)
		}
	}
}
