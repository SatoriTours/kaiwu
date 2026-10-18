// Package config: GitHub credential plumbing is service-environment only and
// never serialized into workbench JSON output or task snapshots.
package config

// GitHub holds service credential plumbing. It is assembled once at startup
// and injected into the credential resolver; it is not part of Workbench
// state and is never written to the database or browser.
type GitHub struct {
	APIToken        string
	GitToken        string
	SSHKey          string
	SSHAgentAllowed bool
}

// LoadGitHub reads credential sources from the service environment. Empty
// values simply disable the corresponding capability.
func LoadGitHub(lookup func(string) (string, bool)) GitHub {
	get := func(k string) string { v, _ := lookup(k); return v }
	return GitHub{
		APIToken:        get("KAIWU_GITHUB_API_TOKEN"),
		GitToken:        get("KAIWU_GITHUB_GIT_TOKEN"),
		SSHKey:          get("KAIWU_GITHUB_SSH_KEY"),
		SSHAgentAllowed: get("KAIWU_GITHUB_SSH_AGENT") == "allowed",
	}
}
