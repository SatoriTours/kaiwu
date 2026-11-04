package aiconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestResolveInheritanceDisableAndNoEscalation(t *testing.T) {
	g := Config{SchemaVersion: 1, Adapter: ptr("pi"), Profile: &ProfilePatch{Provider: ptr("allowed"), Model: ptr("original"), Thinking: ptr("high")}, Rules: ptr([]string{"global"}), Skills: ptr([]string{"review"}), CredentialRef: ptr("server_pi")}
	p := Config{SchemaVersion: 1, Profile: &ProfilePatch{Model: ptr("project"), Thinking: ptr("off")}, Rules: ptr([]string{}), Skills: ptr([]string{})}
	r, err := Resolve(g, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile.Provider != "allowed" || r.Profile.Model != "project" || r.Profile.Thinking != "off" || len(r.Rules) != 0 || len(r.Skills) != 0 || r.Sources["profile.model"] != "project" || r.Sources["profile.provider"] != "global" || r.Version == "" {
		t.Fatalf("bad inheritance: %+v", r)
	}
	saved, _ := json.Marshal(r)
	*g.Profile.Model = "changed"
	again, _ := Resolve(g, p)
	if r.Profile.Model != "project" || again.Version == r.Version {
		t.Fatal("template mutation changed old snapshot or lost provenance")
	}
	for _, bad := range []Config{{SchemaVersion: 2}, {SchemaVersion: 1, Adapter: ptr("codex")}, {SchemaVersion: 1, Skills: ptr([]string{"../../private"})}, {SchemaVersion: 1, Profile: &ProfilePatch{Provider: ptr("other")}}, {SchemaVersion: 1, CredentialRef: ptr("other-project")}} {
		if _, err := Resolve(g, bad); err == nil {
			t.Fatalf("accepted privilege/schema change: %+v", bad)
		}
	}
	if strings.Contains(string(saved), "apiKey") || strings.Contains(string(saved), "accessToken") {
		t.Fatal("credential body in snapshot")
	}
}
func TestServerCredentialReferenceRequiresBoundProvider(t *testing.T) {
	ref := "server_pi"
	if _, err := Resolve(Config{SchemaVersion: 1, CredentialRef: &ref}, Config{SchemaVersion: 1}); err == nil {
		t.Fatal("unbound server credentials accepted")
	}
}
func TestRejectUnknownAndCredentialBodies(t *testing.T) {
	for _, body := range []string{`{"schemaVersion":0}`, `{"schemaVersion":1,"apiKey":"synthetic"}`, `{"schemaVersion":1,"profile":{"provider":"x","token":"synthetic"}}`, `{"schemaVersion":1} {}`} {
		if _, err := Decode([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
