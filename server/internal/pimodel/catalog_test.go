package pimodel

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCredentialIsolationAndCommandRejection(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	auth := `{"a":{"type":"api_key","key":"private-secret"},"b":{"type":"oauth","access":"other-secret","refresh":"refresh","expires":42}}`
	if e := os.WriteFile(filepath.Join(source, "auth.json"), []byte(auth), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "models.json"), []byte(`{"providers":{"a":{"models":[{"id":"test"}]},"b":{"apiKey":"other-secret"}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "settings.json"), []byte(`{"extensions":["unsafe"]}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Prepare(source, target, "a"); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"auth.json", "models.json"} {
		b, e := os.ReadFile(filepath.Join(target, name))
		if e != nil || strings.Contains(string(b), "other-secret") {
			t.Fatal("other provider leaked", e)
		}
		info, _ := os.Stat(filepath.Join(target, name))
		if info.Mode().Perm() != 0600 {
			t.Fatal("unsafe permissions")
		}
	}
	if _, e := os.Stat(filepath.Join(target, "settings.json")); !os.IsNotExist(e) {
		t.Fatal("settings copied")
	}
	_ = os.WriteFile(filepath.Join(target, "auth.json"), []byte(`{"a":{"type":"api_key","key":"refreshed"}}`), 0600)
	if e := Prepare(source, target, "a"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(target, "auth.json"))
	if !strings.Contains(string(b), "refreshed") {
		t.Fatal("private refreshed credential overwritten")
	}
	b, _ = os.ReadFile(filepath.Join(source, "auth.json"))
	if string(b) != auth {
		t.Fatal("source modified")
	}
	_ = os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"a":{"type":"api_key","key":"!touch /tmp/forbidden"}}`), 0600)
	if e := Prepare(source, t.TempDir(), ""); e == nil || strings.Contains(e.Error(), "touch") {
		t.Fatal("command accepted or leaked")
	}
}

func TestCatalogUsesRPCAndReturnsMetadataOnly(t *testing.T) {
	program := filepath.Join(t.TempDir(), "fakepi")
	cmd := exec.Command("go", "build", "-o", program, "../agent/testdata/fakepi")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture build: %v %s", e, out)
	}
	root := t.TempDir()
	c := Catalog{Program: program, DataDir: root, SourceDir: t.TempDir()}
	models, e := c.List(context.Background())
	if e != nil || len(models) != 3 {
		t.Fatalf("models: %+v %v", models, e)
	}
	b, _ := json.Marshal(models)
	if strings.Contains(string(b), "must-not") || strings.Contains(string(b), "apiKey") {
		t.Fatal("raw model fields leaked")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("probe or credentials left behind")
	}
	models[0].ID = "mutated"
	again, e := c.List(context.Background())
	if e != nil || again[0].ID == "mutated" {
		t.Fatal("cache not copied")
	}
	c.Program = "/missing/pi"
	c.expires = time.Time{}
	if _, e = c.List(context.Background()); e == nil {
		t.Fatal("missing executable accepted")
	}
}

// Explicit, opt-in metadata-only check. Never sends prompt or loads extensions.
func TestInstalledPiCatalog(t *testing.T) {
	program := os.Getenv("KAIWU_TEST_REAL_PI")
	if program == "" {
		t.Skip("real pi metadata query is opt-in")
	}
	c := Catalog{Program: program, SourceDir: os.Getenv("KAIWU_TEST_PI_DIR"), DataDir: t.TempDir()}
	models, e := c.List(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	for _, m := range models {
		counts[m.Provider]++
	}
	t.Logf("real pi metadata: %d models, providers %v", len(models), counts)
}
