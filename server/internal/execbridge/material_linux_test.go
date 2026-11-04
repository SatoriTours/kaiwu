//go:build linux

package execbridge

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMaterialPersistsKeyAndPublicDirectoryWithoutRotation(t *testing.T) {
	root := privateMaterialRoot(t)
	b, _, _ := authFixture(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	public := m.Public()
	key, err := m.SigningKey()
	if err != nil || !key.Public().(ed25519.PublicKey).Equal(public.Bootstrap.HostPublicKey) {
		t.Fatal("key/public binding lost", err)
	}
	directory := m.DirectoryIdentity()
	if directory.Inode == 0 {
		t.Fatal("missing rendezvous identity")
	}
	raw, err := os.ReadFile(filepath.Join(root, b.Identity.RunID, "public", "public.json"))
	if err != nil || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(key)) {
		t.Fatal("private key entered public bootstrap")
	}
	if info, err := os.Stat(filepath.Join(root, b.Identity.RunID, "host.key")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key not private")
	}
	m.Close()
	again, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	restored, err := again.SigningKey()
	if err != nil || !key.Equal(restored) || directory != again.DirectoryIdentity() {
		t.Fatal("restart rotated key or rendezvous")
	}
	// Public copies cannot mutate the key used for the next connection.
	public.Bootstrap.HostPublicKey[0] ^= 1
	if public.Bootstrap.HostPublicKey.Equal(again.Public().Bootstrap.HostPublicKey) {
		t.Fatal("mutable public key alias")
	}
	if _, err = PrepareRunMaterial(context.Background(), root, b.Identity, strings.Repeat("c", 64)); err == nil {
		t.Fatal("run rebound to different spec")
	}
}
func TestRunMaterialRefusesUnknownAndCorruptOwnership(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	if err := os.Mkdir(filepath.Join(root, b.Identity.RunID), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash); err == nil {
		t.Fatal("adopted unknown directory")
	}
	root = privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	keyPath := filepath.Join(root, b.Identity.RunID, "host.key")
	if err = os.WriteFile(keyPath, []byte("never-reflect-corrupt-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash); err == nil || strings.Contains(err.Error(), "never-reflect") {
		t.Fatal("corrupt key regenerated or reflected")
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil || string(raw) != "never-reflect-corrupt-key" {
		t.Fatal("failure destroyed evidence")
	}
}
func TestPublicBootstrapRejectsAliasesUnknownAndSecrets(t *testing.T) {
	b, _, _ := authFixture(t)
	raw, _ := json.Marshal(PublicBootstrap{SchemaVersion: 1, Bootstrap: b, Socket: "control.sock"})
	if _, err := decodePublicBootstrap(raw); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{`{"ſchemaVersion":1}`, `{"SchemaVersion":1,"schemaversion":1}`, string(raw) + ` {}`, `{"SchemaVersion":1,"PrivateKey":"never-reflect-private-key"}`} {
		if _, err := decodePublicBootstrap([]byte(invalid)); err == nil || strings.Contains(err.Error(), "never-reflect") {
			t.Fatal("ambiguous/secret bootstrap accepted or echoed")
		}
	}
}

func privateMaterialRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPublishedBootstrapCannotRecreateOrRebindDirectory(t *testing.T) {
	b, _, _ := authFixture(t)
	for _, replace := range []bool{false, true} {
		root := privateMaterialRoot(t)
		m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
		if err != nil {
			t.Fatal(err)
		}
		m.Close()
		public := filepath.Join(root, b.Identity.RunID, "public")
		if err = os.Rename(public, public+"-old"); err != nil {
			t.Fatal(err)
		}
		if replace {
			if err = os.Mkdir(public, 0700); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(filepath.Join(public+"-old", "public.json"))
			if err = os.WriteFile(filepath.Join(public, "public.json"), raw, 0444); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash); err == nil {
			t.Fatal("published rendezvous recreated/rebound")
		}
	}
}
func TestPublicDirectoryCannotExposeUnknownMaterial(t *testing.T) {
	b, _, _ := authFixture(t)
	root := privateMaterialRoot(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if err = os.WriteFile(filepath.Join(root, b.Identity.RunID, "public", "unexpected-private"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash); err == nil {
		t.Fatal("public mount admitted unknown material")
	}
}

func TestRunMaterialRejectsSymlinkHardlinkAndPublicPermissions(t *testing.T) {
	b, _, _ := authFixture(t)
	for _, mode := range []string{"ancestor", "key-link", "key-hardlink", "wide-key", "public-link"} {
		root := privateMaterialRoot(t)
		m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
		if err != nil {
			t.Fatal(err)
		}
		m.Close()
		run := filepath.Join(root, b.Identity.RunID)
		switch mode {
		case "ancestor":
			old := root + "-moved"
			if err = os.Rename(root, old); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(old) })
			if err = os.Symlink(old, root); err != nil {
				t.Fatal(err)
			}
		case "key-link":
			if err = os.Rename(filepath.Join(run, "host.key"), filepath.Join(run, "old.key")); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink("old.key", filepath.Join(run, "host.key")); err != nil {
				t.Fatal(err)
			}
		case "key-hardlink":
			if err = os.Link(filepath.Join(run, "host.key"), filepath.Join(run, "copy.key")); err != nil {
				t.Fatal(err)
			}
		case "wide-key":
			if err = os.Chmod(filepath.Join(run, "host.key"), 0644); err != nil {
				t.Fatal(err)
			}
		case "public-link":
			if err = os.Rename(filepath.Join(run, "public"), filepath.Join(run, "old-public")); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink("old-public", filepath.Join(run, "public")); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash); err == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
}
