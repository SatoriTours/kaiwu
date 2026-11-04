package pimodel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionsPrivatePersistenceAndRevision(t *testing.T) {
	root := t.TempDir()
	c, err := OpenConnections(root)
	if err != nil {
		t.Fatal(err)
	}
	req := ConnectionInput{Name: "我的中转", API: "openai-completions", BaseURL: "https://example.com/v1", Key: "synthetic-key", Models: []string{"test/model"}}
	saved, err := c.Save(req)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Provider == "" || saved.ID == "" || !saved.HasKey || saved.Revision != 1 {
		t.Fatalf("bad metadata: %+v", saved)
	}
	b, _ := json.Marshal(c.List())
	if strings.Contains(string(b), "synthetic-key") {
		t.Fatal("secret returned")
	}
	info, err := os.Stat(filepath.Join(root, "pi-connections.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private file", err)
	}
	reopened, err := OpenConnections(root)
	if err != nil || len(reopened.List()) != 1 {
		t.Fatal("not durable", err)
	}
	req.ID = saved.ID
	req.Revision = 1
	req.Key = ""
	req.Name = "changed"
	updated, err := c.Save(req)
	if err != nil || updated.Revision != 2 || !updated.HasKey {
		t.Fatal("blank key should preserve", err)
	}
	if _, err = c.Save(req); err == nil {
		t.Fatal("stale edit accepted")
	}
	if err = c.Remove(saved.ID, 1); err == nil {
		t.Fatal("stale remove accepted")
	}
	if err = c.Remove(saved.ID, 2); err != nil || len(c.List()) != 0 {
		t.Fatal("remove", err)
	}
}
func TestConnectionProbeUsesSelectedProtocolAndRedactsErrors(t *testing.T) {
	var path, auth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"one","apiKey":"SECRET"},{"id":"one"},{"id":"two"}]}`))
	}))
	defer upstream.Close()
	c, _ := OpenConnections(t.TempDir())
	models, err := c.Discover(context.Background(), ConnectionInput{API: "openai-completions", BaseURL: upstream.URL + "/v1", Key: "probe-secret"})
	if err != nil || len(models) != 2 || path != "/v1/models" || auth != "Bearer probe-secret" {
		t.Fatalf("probe: %v %v %s", models, err, path)
	}
	b, _ := json.Marshal(models)
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("raw fields leaked")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "probe-secret", 401) }))
	defer bad.Close()
	_, err = c.Discover(context.Background(), ConnectionInput{API: "openai-completions", BaseURL: bad.URL, Key: "probe-secret"})
	if err == nil || strings.Contains(err.Error(), "probe-secret") {
		t.Fatal("upstream error leak", err)
	}
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	if _, err = c.Discover(context.Background(), ConnectionInput{API: "openai-completions", BaseURL: redirect.URL, Key: "probe-secret"}); err == nil || redirected {
		t.Fatal("redirect followed")
	}
}
func TestManagedConnectionsLayerWithoutChangingSourceOrSession(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	conn, err := c.Save(ConnectionInput{Name: "my", API: "anthropic-messages", BaseURL: "https://example.com", Key: "$TOKEN!literal", Models: []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	source, target := t.TempDir(), t.TempDir()
	original := `{"personal":{"type":"api_key","key":"personal-secret"}}`
	os.WriteFile(filepath.Join(source, "auth.json"), []byte(original), 0600)
	if err = c.Prepare(source, target, conn.Provider); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(target, "auth.json"))
	if strings.Contains(string(data), "personal-secret") || !strings.Contains(string(data), "$$TOKEN") {
		t.Fatal("wrong isolated credential", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(target, "models.json"))
	if !strings.Contains(string(data), conn.Provider) || !strings.Contains(string(data), "anthropic-messages") {
		t.Fatal("missing model", string(data))
	}
	os.WriteFile(filepath.Join(target, "auth.json"), []byte(`{"refreshed":"preserve"}`), 0600)
	if err = c.Prepare(source, target, conn.Provider); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(target, "auth.json"))
	if string(data) != `{"refreshed":"preserve"}` {
		t.Fatal("session replaced")
	}
	data, _ = os.ReadFile(filepath.Join(source, "auth.json"))
	if string(data) != original {
		t.Fatal("source modified")
	}
}
func TestConnectionValidationRejectsUnsafeInputs(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	for _, url := range []string{"file:///tmp/auth", "https://user:pass@example.com", "https://example.com/?key=secret", "https://example.com/#secret", "http://169.254.169.254/latest"} {
		if _, err := c.Save(ConnectionInput{Name: "test", API: "openai-completions", BaseURL: url, Key: "x", Models: []string{"one"}}); err == nil {
			t.Errorf("accepted %s", url)
		}
	}
	for _, key := range []string{"abc\r\nInjected: y", strings.Repeat("a", 17000)} {
		if _, err := c.Save(ConnectionInput{Name: "test", API: "openai-completions", BaseURL: "https://example.com", Key: key, Models: []string{"one"}}); err == nil {
			t.Fatal("invalid key")
		}
	}
}
