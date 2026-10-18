package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGitHubConfig: credential plumbing is environment-sourced, never part
// of workbench JSON, and the agent allowance is explicit.
func TestGitHubConfig(t *testing.T) {
	env := map[string]string{
		"KAIWU_GITHUB_API_TOKEN": "api",
		"KAIWU_GITHUB_GIT_TOKEN": "git",
		"KAIWU_GITHUB_SSH_KEY":   "/keys/k",
		"KAIWU_GITHUB_SSH_AGENT": "allowed",
	}
	g := LoadGitHub(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if g.APIToken != "api" || g.GitToken != "git" || g.SSHKey != "/keys/k" || !g.SSHAgentAllowed {
		t.Fatalf("LoadGitHub: %+v", g)
	}
	empty := LoadGitHub(func(string) (string, bool) { return "", false })
	if empty.APIToken != "" || empty.SSHAgentAllowed {
		t.Fatalf("empty LoadGitHub: %+v", empty)
	}
	// Serialized workbench configuration never carries GitHub credentials.
	out, e := json.Marshal(Workbench{})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(out), "github") || strings.Contains(string(out), "token") {
		t.Fatalf("workbench JSON contains credential plumbing: %s", out)
	}
}
