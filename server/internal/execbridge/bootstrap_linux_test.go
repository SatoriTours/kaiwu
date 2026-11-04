//go:build linux

package execbridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeBootstrapReadsOnlyPublishedPublicDescription(t *testing.T) {
	root := privateMaterialRoot(t)
	b, _, _ := authFixture(t)
	m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	dir := filepath.Join(root, b.Identity.RunID, "public")
	got, err := LoadPublicBootstrap(context.Background(), dir)
	if err != nil || got.Bootstrap.Identity != b.Identity || got.Bootstrap.SpecHash != b.SpecHash || !got.Bootstrap.HostPublicKey.Equal(m.Public().Bootstrap.HostPublicKey) {
		t.Fatal("public bootstrap not loaded", err)
	}
	for _, name := range []string{"../public", "."} {
		if _, err = LoadPublicBootstrap(context.Background(), dir+"/"+name); err == nil {
			t.Fatal("noncanonical path accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = LoadPublicBootstrap(ctx, dir); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestBridgeBootstrapRejectsWritableLinkedAndUnknownEntries(t *testing.T) {
	for _, mode := range []string{"writable", "link", "ancestor", "unknown", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			root := privateMaterialRoot(t)
			b, _, _ := authFixture(t)
			m, err := PrepareRunMaterial(context.Background(), root, b.Identity, b.SpecHash)
			if err != nil {
				t.Fatal(err)
			}
			m.Close()
			dir := filepath.Join(root, b.Identity.RunID, "public")
			file := filepath.Join(dir, "public.json")
			switch mode {
			case "writable":
				err = os.Chmod(file, 0644)
			case "link":
				if err = os.Rename(file, filepath.Join(dir, "old")); err == nil {
					err = os.Symlink("old", file)
				}
			case "ancestor":
				if err = os.Rename(dir, dir+"-old"); err == nil {
					err = os.Symlink("public-old", dir)
				}
			case "unknown":
				err = os.WriteFile(filepath.Join(dir, "private.key"), []byte("synthetic"), 0600)
			case "oversize":
				if err = os.Chmod(file, 0600); err == nil {
					err = os.WriteFile(file, make([]byte, 16385), 0600)
				}
				if err == nil {
					err = os.Chmod(file, 0444)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = LoadPublicBootstrap(context.Background(), dir); err == nil {
				t.Fatal("unsafe bootstrap accepted")
			}
		})
	}
}
