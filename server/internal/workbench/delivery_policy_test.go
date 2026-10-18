package workbench

import (
	"strings"
	"testing"
	"time"
)

func validDeliveryPolicy() ProjectDeliveryPolicy {
	p := DefaultDeliveryPolicy("project")
	p.Enabled = true
	p.Repository = DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	p.Checks = []DeliveryCheck{{ID: "test", Argv: []string{"go", "test", "./..."}, Dir: ".", TimeoutSeconds: 300}}
	p.TaskWorkflows = []DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	p.MainWorkflows = []DeliveryWorkflow{{ID: 2}}
	p.GitCredentialRef = "server_github_https"
	p.APICredentialRef = "server_github_api"
	p.AuthorizationDigest = "explicit-scope"
	p.Actor = "local-login"
	p.AuthorizedAt = time.Now().UTC()
	return p
}

func TestDeliveryPolicyDefaultsAndValidation(t *testing.T) {
	p := DefaultDeliveryPolicy("p")
	if p.Enabled || !p.RequireAcceptance || p.MaxRepairs != 3 || p.ActiveSeconds != 3600 || p.TriggerSeconds != 300 || p.CISeconds != 1800 {
		t.Fatalf("defaults: %+v", p)
	}
	if err := ValidateDeliveryPolicy(p); err != nil {
		t.Fatal(err)
	}
	p.Enabled = true
	if ValidateDeliveryPolicy(p) == nil {
		t.Fatal("unconfigured enabled policy accepted")
	}
	if err := ValidateDeliveryPolicy(validDeliveryPolicy()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*ProjectDeliveryPolicy){
		"repository":         func(p *ProjectDeliveryPolicy) { p.Repository.ID = 0 },
		"authorization":      func(p *ProjectDeliveryPolicy) { p.AuthorizationDigest = "" },
		"actor":              func(p *ProjectDeliveryPolicy) { p.Actor = "" },
		"revoked":            func(p *ProjectDeliveryPolicy) { p.RevokedAt = time.Now() },
		"checks":             func(p *ProjectDeliveryPolicy) { p.Checks = nil },
		"task workflows":     func(p *ProjectDeliveryPolicy) { p.TaskWorkflows = nil },
		"main workflows":     func(p *ProjectDeliveryPolicy) { p.MainWorkflows = nil },
		"timeout":            func(p *ProjectDeliveryPolicy) { p.Checks[0].TimeoutSeconds = 0 },
		"argv":               func(p *ProjectDeliveryPolicy) { p.Checks[0].Argv = nil },
		"nul":                func(p *ProjectDeliveryPolicy) { p.Checks[0].Argv = []string{"go\x00"} },
		"escape cwd":         func(p *ProjectDeliveryPolicy) { p.Checks[0].Dir = "../elsewhere" },
		"absolute cwd":       func(p *ProjectDeliveryPolicy) { p.Checks[0].Dir = "/tmp" },
		"branch":             func(p *ProjectDeliveryPolicy) { p.Repository.MainBranch = "main:other" },
		"duplicate workflow": func(p *ProjectDeliveryPolicy) { p.TaskWorkflows = append(p.TaskWorkflows, p.TaskWorkflows[0]) },
		"duplicate job":      func(p *ProjectDeliveryPolicy) { p.TaskWorkflows[0].Jobs = []string{"test", "test"} },
		"budget":             func(p *ProjectDeliveryPolicy) { p.MaxRepairs = 0 },
		"active budget":      func(p *ProjectDeliveryPolicy) { p.ActiveSeconds = 0 },
		"trigger deadline":   func(p *ProjectDeliveryPolicy) { p.TriggerSeconds = -1 },
		"ci deadline":        func(p *ProjectDeliveryPolicy) { p.CISeconds = 0 },
		"other host":         func(p *ProjectDeliveryPolicy) { p.Repository.URL = "https://example.com/owner/repo" },
		"credentials url":    func(p *ProjectDeliveryPolicy) { p.Repository.URL = "https://secret@github.com/owner/repo" },
		"wrong repository":   func(p *ProjectDeliveryPolicy) { p.Repository.URL = "https://github.com/owner/other" },
		"helper ref":         func(p *ProjectDeliveryPolicy) { p.GitCredentialRef = "$(env)" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := validDeliveryPolicy()
			mutate(&p)
			if ValidateDeliveryPolicy(p) == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	for _, raw := range []string{"git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo.git", "https://github.com/owner/repo"} {
		p := validDeliveryPolicy()
		p.Repository.URL = raw
		if strings.HasPrefix(raw, "ssh:") || strings.HasPrefix(raw, "git@") {
			p.GitCredentialRef = "server_github_ssh"
		}
		if err := ValidateDeliveryPolicy(p); err != nil {
			t.Fatalf("valid URL %s: %v", raw, err)
		}
	}
}

func TestDeliveryPolicyDisabledDraftStillRejectsSecrets(t *testing.T) {
	p := DefaultDeliveryPolicy("p")
	p.Repository.URL = "https://secret@github.com/owner/repo"
	if ValidateDeliveryPolicy(p) == nil {
		t.Fatal("disabled draft can persist credentials in URL")
	}
}

func TestDeliveryPolicyActivePhases(t *testing.T) {
	for _, phase := range []string{"queued", "developing", "local_checking", "committing", "pushing_task", "task_ci", "preparing_integration", "awaiting_acceptance", "integrating", "integration_checking", "pushing_main", "main_ci", "repairing", "blocked", "paused", "needs_check"} {
		if !DeliveryActive(phase) {
			t.Fatalf("active phase %s", phase)
		}
	}
	for _, phase := range []string{"succeeded", "cancelled", "", "unknown"} {
		if DeliveryActive(phase) {
			t.Fatalf("nonactive phase %s", phase)
		}
	}
}
