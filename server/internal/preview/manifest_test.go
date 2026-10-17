//go:build linux

package preview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestManifestRejectsFIFOWithoutBlocking(t *testing.T) {
	tree := t.TempDir()
	name := filepath.Join(tree, "kaiwu-preview.json")
	if e := syscall.Mkfifo(name, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, _, e := readManifest(tree); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(100 * time.Millisecond):
		// Release the faulty blocking open so even the RED run cleans up.
		f, e := os.OpenFile(name, os.O_RDWR, 0600)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		<-done
		t.Fatal("manifest FIFO blocked manager instead of being rejected")
	}
}

func TestManifestPathCannotEscapeGatewayPrefix(t *testing.T) {
	for _, path := range []string{"/../api/session", "/%2e%2e/api/session", "/nested/../../api/session", "/%5c..%5capi/session"} {
		t.Run(path, func(t *testing.T) {
			tree := t.TempDir()
			b, _ := json.Marshal(map[string]any{"version": 1, "command": []string{"program", "{port}"}, "path": path})
			_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), b, 0600)
			if _, _, e := readManifest(tree); e == nil {
				t.Fatal("manifest path can leave token prefix", path)
			}
		})
	}
}
