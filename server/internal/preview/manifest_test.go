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

func writeManifestFile(t *testing.T, tree, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestManifestV1KeepsCommandContract(t *testing.T) {
	tree := t.TempDir()
	writeManifestFile(t, tree, `{"version":1,"command":["npm","run","dev","--port","{port}"],"path":"/"}`)
	v, h, e := readManifest(tree)
	if e != nil {
		t.Fatal(e)
	}
	if v.Kind != "" || v.Root != "" || v.Entry != "" || len(v.Command) != 5 || v.Command[0] != "npm" || v.Path != "/" {
		t.Fatalf("v1 contract drifted: %+v", v)
	}
	if h == [32]byte{} {
		t.Fatal("v1 hash missing")
	}
}

func TestManifestV2StaticAccepted(t *testing.T) {
	tree := t.TempDir()
	writeManifestFile(t, tree, `{"version":2,"kind":"static","entry":"index.html"}`)
	v, h, e := readManifest(tree)
	if e != nil {
		t.Fatal(e)
	}
	if v.Kind != "static" || v.Root != "." || v.Entry != "index.html" || len(v.Command) != 0 || v.Path != "/" {
		t.Fatalf("static defaults wrong: %+v", v)
	}
	if h == [32]byte{} {
		t.Fatal("static hash missing")
	}

	writeManifestFile(t, tree, `{"version":2,"kind":"static","root":"public","entry":"docs/index.html","path":"/docs/"}`)
	v, _, e = readManifest(tree)
	if e != nil {
		t.Fatal(e)
	}
	if v.Root != "public" || v.Entry != "docs/index.html" || v.Path != "/docs/" {
		t.Fatalf("static root/entry/path wrong: %+v", v)
	}
}

func TestManifestRejectsInvalidStatic(t *testing.T) {
	cases := map[string]string{
		"command mixed":      `{"version":2,"kind":"static","entry":"index.html","command":["node","{port}"]}`,
		"missing entry":      `{"version":2,"kind":"static"}`,
		"empty entry":        `{"version":2,"kind":"static","entry":""}`,
		"entry traversal":    `{"version":2,"kind":"static","entry":"../index.html"}`,
		"entry dot segment":  `{"version":2,"kind":"static","entry":"docs/./index.html"}`,
		"entry absolute":     `{"version":2,"kind":"static","entry":"/index.html"}`,
		"entry backslash":    `{"version":2,"kind":"static","entry":"a\\b.html"}`,
		"entry not html":     `{"version":2,"kind":"static","entry":"notes.txt"}`,
		"root absolute":      `{"version":2,"kind":"static","root":"/tmp","entry":"index.html"}`,
		"root traversal":     `{"version":2,"kind":"static","root":"../up","entry":"index.html"}`,
		"root backslash":     `{"version":2,"kind":"static","root":"a\\b","entry":"index.html"}`,
		"root dot segment":   `{"version":2,"kind":"static","root":"a/./b","entry":"index.html"}`,
		"unknown kind":       `{"version":2,"kind":"server","entry":"index.html"}`,
		"missing kind":       `{"version":2,"entry":"index.html"}`,
		"v1 with static":     `{"version":1,"kind":"static","entry":"index.html","path":"/"}`,
		"v2 unknown field":   `{"version":2,"kind":"static","entry":"index.html","port":8080}`,
		"v2 bad static path": `{"version":2,"kind":"static","entry":"index.html","path":"//evil"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			tree := t.TempDir()
			writeManifestFile(t, tree, body)
			if _, _, e := readManifest(tree); e == nil {
				t.Fatalf("accepted invalid manifest: %s", body)
			}
		})
	}
}
