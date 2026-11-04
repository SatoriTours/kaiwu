// Package aiconfig resolves declarative pi configuration; it never stores credentials.
package aiconfig

type Profile struct {
	CredentialSource string `json:"credentialSource,omitempty"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	Thinking         string `json:"thinking"`
}
type ProfilePatch struct {
	Provider *string `json:"provider,omitempty"`
	Model    *string `json:"model,omitempty"`
	Thinking *string `json:"thinking,omitempty"`
}
type Config struct {
	SchemaVersion int           `json:"schemaVersion"`
	Adapter       *string       `json:"adapter,omitempty"`
	Profile       *ProfilePatch `json:"profile,omitempty"`
	Rules         *[]string     `json:"rules,omitempty"`
	Skills        *[]string     `json:"skills,omitempty"`
	CredentialRef *string       `json:"credentialRef,omitempty"`
}
type Resolved struct {
	SkillContents map[string]string `json:"skillContents,omitempty"`
	SchemaVersion int               `json:"schemaVersion"`
	Adapter       string            `json:"adapter"`
	Profile       Profile           `json:"profile"`
	Rules         []string          `json:"rules"`
	Skills        []string          `json:"skills"`
	CredentialRef string            `json:"credentialRef,omitempty"`
	Sources       map[string]string `json:"sources"`
	Version       string            `json:"version"`
}

func FromProfile(p Profile) Config {
	return Config{SchemaVersion: 1, Profile: &ProfilePatch{Provider: &p.Provider, Model: &p.Model, Thinking: &p.Thinking}, CredentialRef: &p.CredentialSource}
}
