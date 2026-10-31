//go:build !windows

package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestSSHRemoteCanonicalIdentityAndProductionChild(t *testing.T) {
	for _, raw := range []string{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo.git"} {
		for _, agent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/agent=%t", raw, agent), func(t *testing.T) {
				f := reviewFixture(t)
				g(t, f.project.Path, "config", "remote.origin.url", raw)
				canonical := "ssh://git@github.com/owner/repo.git"
				policy := workbench.ProjectDeliveryPolicy{Repository: workbench.DeliveryRepository{ID: 123, URL: canonical, MainBranch: "main"}}
				target, err := ResolveRemote(context.Background(), f.project, policy, "0123456789abcdef0123456789abcdef")
				if err != nil {
					t.Fatal(err)
				}
				if target.URL != canonical {
					t.Fatalf("noncanonical target: %q", target.URL)
				}
				manual, err := ResolveManualRemote(context.Background(), f.project)
				if err != nil || manual.URL != canonical || manual.CredentialRef != githubauth.RefSSH {
					t.Fatalf("manual target: %+v %v", manual, err)
				}
				bin := t.TempDir()
				record := filepath.Join(bin, "ssh-argv")
				socketRecord := filepath.Join(bin, "socket")
				ref := "refs/heads/main"
				packet := fmt.Sprintf("%04x%s %s\\000\\n0000", 4+len(testSHA5)+1+len(ref)+2, testSHA5, ref)
				script := "#!/bin/sh\nprintf '%s\\000' \"$@\" > '" + record + "'\nprintf '%s' \"${SSH_AUTH_SOCK-unset}\" > '" + socketRecord + "'\nprintf '" + packet + "'\ncat >/dev/null\n"
				if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
				key := filepath.Join(bin, "synthetic key")
				if err := os.WriteFile(key, []byte("synthetic"), 0o600); err != nil {
					t.Fatal(err)
				}
				env := map[string]string{"KAIWU_GITHUB_SSH_KEY": key}
				sock := filepath.Join(bin, "authorized.sock")
				if agent {
					env = map[string]string{"KAIWU_GITHUB_SSH_AGENT": "allowed", "SSH_AUTH_SOCK": sock}
				}
				resolver, _ := githubauth.New(githubauth.Config{Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("SSH_AUTH_SOCK", "/ambient/unapproved-agent.sock")
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				lease := &boundaryLease{}
				sha, err := NewRemoteRunner(RemoteConfig{Resolver: resolver}).Observe(ctx, f.project.Path, target, ref, githubauth.RefSSH, lease)
				if err != nil || sha != testSHA5 {
					t.Fatalf("actual Git→SSH child: sha=%q err=%v", sha, err)
				}
				data, err := os.ReadFile(record)
				if err != nil {
					t.Fatal(err)
				}
				args := strings.Split(string(data), "\x00")
				foundHost, foundPack, isolated := false, false, false
				for i, arg := range args {
					foundHost = foundHost || arg == "git@github.com"
					foundPack = foundPack || arg == "git-upload-pack '/owner/repo.git'"
					isolated = isolated || (arg == "-F" && i+1 < len(args) && args[i+1] == "none")
				}
				if !foundHost || !foundPack || !isolated || lease.started != 1 {
					t.Fatalf("SSH destination/options/lifecycle not preserved: %v lease=%+v", args, lease)
				}
				gotSocket, err := os.ReadFile(socketRecord)
				if err != nil {
					t.Fatal(err)
				}
				wantSocket := "unset"
				if agent {
					wantSocket = sock
				}
				if string(gotSocket) != wantSocket {
					t.Fatal("SSH inherited a socket other than the explicitly authorized source")
				}
			})
		}
	}
}
