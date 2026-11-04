package pimodel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitModelChangeAddsProviderWithoutReplacingOldSnapshot(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	source, target := t.TempDir(), t.TempDir()
	one, _ := c.Save(ConnectionInput{Name: "one", API: "openai-completions", BaseURL: "https://one.example", Key: "one-secret", Models: []string{"one"}})
	two, _ := c.Save(ConnectionInput{Name: "two", API: "openai-completions", BaseURL: "https://two.example", Key: "two-secret", Models: []string{"two"}})
	if e := c.Prepare(source, target, one.Provider); e != nil {
		t.Fatal(e)
	}
	if e := c.AddProvider(source, target, two.Provider); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(target, "auth.json"))
	if !strings.Contains(string(b), "one-secret") || !strings.Contains(string(b), "two-secret") {
		t.Fatal("missing provider")
	}
	_, e := c.Save(ConnectionInput{ID: one.ID, Revision: one.Revision, Name: one.Name, API: one.API, BaseURL: one.BaseURL, Key: "changed-key", Models: one.Models})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.AddProvider(source, target, one.Provider); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(filepath.Join(target, "auth.json"))
	if strings.Contains(string(b), "changed-key") {
		t.Fatal("original snapshot replaced")
	}
}
func TestCorruptHostIdentityFailsClosed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pi-connections.json"), []byte(`{"version":1,"hostId":"x","records":[]}`), 0600)
	if _, e := OpenConnections(dir); e == nil {
		t.Fatal("bad host ID accepted")
	}
}
func TestOAuthDoesNotInheritSourceProviderProxy(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	raw := json.RawMessage(`{"type":"oauth","access":"a","refresh":"b","expires":9999999999999}`)
	c.data.Records = []connectionRecord{{Connection: Connection{ID: "test", Provider: "openai", Kind: "oauth", HasKey: true}, Auth: raw}}
	source, target := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(source, "models.json"), []byte(`{"providers":{"openai":{"baseUrl":"https://source-proxy.example/v1","headers":{"secret":"old"}}}}`), 0600)
	if e := c.Prepare(source, target, "openai"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(target, "models.json"))
	if strings.Contains(string(b), "source-proxy") {
		t.Fatal("OAuth inherited an unrelated credential target")
	}
}
func TestInstalledPiManagedMetadata(t *testing.T) {
	program := os.Getenv("KAIWU_TEST_REAL_PI")
	if program == "" {
		t.Skip("opt-in, metadata only")
	}
	c, _ := OpenConnections(t.TempDir())
	conn, e := c.Save(ConnectionInput{Name: "synthetic", API: "openai-completions", BaseURL: "https://example.invalid/v1", Key: "synthetic-not-a-real-key", Models: []string{"isolated-test-model"}})
	if e != nil {
		t.Fatal(e)
	}
	catalog := Catalog{Program: program, SourceDir: t.TempDir(), DataDir: t.TempDir(), Connections: c}
	models, e := catalog.List(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range models {
		if m.Provider == conn.Provider && m.ID == "isolated-test-model" {
			return
		}
	}
	t.Fatal("managed model absent from real pi registry")
}
