//go:build linux

package preview

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func writeStatic(t *testing.T, name string, data []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(name), 0o755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, data, 0o644); e != nil {
		t.Fatal(e)
	}
}

// staticTree builds a small conventional site plus the neighbours a static
// preview must never expose.
func staticTree(t *testing.T) (tree, outside string) {
	t.Helper()
	tree, outside = t.TempDir(), t.TempDir()
	writeStatic(t, filepath.Join(tree, "index.html"), []byte("<!doctype html><title>t</title>"))
	writeStatic(t, filepath.Join(tree, "public/index.html"), []byte("<!doctype html>public"))
	writeStatic(t, filepath.Join(tree, "assets/app.css"), []byte("body{color:red}"))
	writeStatic(t, filepath.Join(tree, "assets/app.js"), []byte("console.log(1)"))
	writeStatic(t, filepath.Join(tree, "assets/main.mjs"), []byte("export const a = 1"))
	writeStatic(t, filepath.Join(tree, "img/pic.png"), []byte("\x89PNG\r\n\x1a\nxxxx"))
	writeStatic(t, filepath.Join(tree, "fonts/f.woff2"), []byte("wOF2xxxx"))
	writeStatic(t, filepath.Join(tree, "app.wasm"), []byte("\x00asm\x01\x00\x00\x00"))
	writeStatic(t, filepath.Join(tree, ".git/config"), []byte("[core]"))
	writeStatic(t, filepath.Join(tree, ".env"), []byte("SECRET=1"))
	writeStatic(t, filepath.Join(tree, ".hidden.html"), []byte("hidden"))
	writeStatic(t, filepath.Join(tree, "node_modules/x/index.js"), []byte("x"))
	writeStatic(t, filepath.Join(tree, "secret.pem"), []byte("key"))
	writeStatic(t, filepath.Join(tree, "id_rsa"), []byte("key"))
	writeStatic(t, filepath.Join(tree, "server/main.go"), []byte("package main"))
	writeStatic(t, filepath.Join(tree, "package.json"), []byte("{}"))
	writeStatic(t, filepath.Join(tree, "Dockerfile"), []byte("FROM x"))
	if e := os.Mkdir(filepath.Join(tree, "assets/sub"), 0o755); e != nil {
		t.Fatal(e)
	}
	writeStatic(t, filepath.Join(outside, "outside.html"), []byte("outside"))
	writeStatic(t, filepath.Join(outside, "leak.css"), []byte("leak"))
	return tree, outside
}

func TestStaticFileServesOnlySafeAssets(t *testing.T) {
	tree, _ := staticTree(t)
	cases := []struct{ root, rel, mime, body string }{
		{".", "index.html", "text/html; charset=utf-8", "<!doctype html><title>t</title>"},
		{"public", "index.html", "text/html; charset=utf-8", "<!doctype html>public"},
		{".", "assets/app.css", "text/css; charset=utf-8", "body{color:red}"},
		{".", "assets/app.js", "text/javascript; charset=utf-8", "console.log(1)"},
		{".", "assets/main.mjs", "text/javascript; charset=utf-8", "export const a = 1"},
		{".", "img/pic.png", "image/png", "\x89PNG\r\n\x1a\nxxxx"},
		{".", "fonts/f.woff2", "font/woff2", "wOF2xxxx"},
		{".", "app.wasm", "application/wasm", "\x00asm\x01\x00\x00\x00"},
	}
	for _, c := range cases {
		f, info, mime, e := openStaticFile(tree, c.root, c.rel)
		if e != nil {
			t.Fatalf("open %s/%s: %v", c.root, c.rel, e)
		}
		b, re := io.ReadAll(f)
		f.Close()
		if re != nil || string(b) != c.body || mime != c.mime || !info.Mode().IsRegular() || info.Size() != int64(len(c.body)) {
			t.Fatalf("open %s/%s: body=%q mime=%q err=%v info=%v", c.root, c.rel, b, mime, re, info)
		}
	}
}

func TestStaticFileRejectsUnsafePaths(t *testing.T) {
	tree, _ := staticTree(t)
	for _, c := range []struct{ root, rel string }{
		{".", ""},
		{".", "."},
		{".", ".."},
		{".", "../x.html"},
		{".", "assets/../../x.html"},
		{".", "/etc/passwd"},
		{".", "//etc/passwd"},
		{".", "assets//app.css"},
		{".", "assets/./app.css"},
		{".", "./index.html"},
		{".", `assets\app.css`},
		{".", "%2e%2e/x.html"},
		{".", "assets%2fapp.css"},
		{".", "assets/%2fetc"},
		{".", "index.html%00.png"},
		{".", "index\x01.html"},
		{".", ".git/config"},
		{".", ".env"},
		{".", ".hidden.html"},
		{".", "node_modules/x/index.js"},
		{".", "secret.pem"},
		{".", "id_rsa"},
		{".", "server/main.go"},
		{".", "package.json"},
		{".", "Dockerfile"},
		{"..", "outside.html"},
		{"/etc", "passwd"},
		{"assets/..", "index.html"},
		{"", "index.html"},
	} {
		if f, _, _, e := openStaticFile(tree, c.root, c.rel); e == nil {
			f.Close()
			t.Fatalf("expected refusal for root=%q rel=%q", c.root, c.rel)
		}
	}
}

func TestStaticFileRejectsSymlinksFIFOsAndDirectories(t *testing.T) {
	tree, outside := staticTree(t)
	if e := os.Symlink(filepath.Join(outside, "outside.html"), filepath.Join(tree, "link.html")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(tree, "linkdir")); e != nil {
		t.Fatal(e)
	}
	if e := unix.Mkfifo(filepath.Join(tree, "pipe.css"), 0o644); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct{ root, rel string }{
		{".", "link.html"},
		{"linkdir", "outside.html"},
		{"linkdir", "leak.css"},
		{".", "linkdir/outside.html"},
		{".", "pipe.css"},
		{".", "assets"},
		{".", "assets/"},
		{".", "assets/sub"},
	} {
		done := make(chan error, 1)
		go func(root, rel string) {
			f, _, _, e := openStaticFile(tree, root, rel)
			if e == nil {
				f.Close()
			}
			done <- e
		}(c.root, c.rel)
		select {
		case e := <-done:
			if e == nil {
				t.Fatalf("expected refusal for root=%q rel=%q", c.root, c.rel)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("open blocked on root=%q rel=%q", c.root, c.rel)
		}
	}
}

func TestStaticFileRejectsOversizedFile(t *testing.T) {
	tree := t.TempDir()
	big := filepath.Join(tree, "big.js")
	writeStatic(t, big, []byte("x"))
	if e := os.Truncate(big, maxStaticFileSize+1); e != nil {
		t.Fatal(e)
	}
	if f, _, _, e := openStaticFile(tree, ".", "big.js"); e == nil {
		f.Close()
		t.Fatal("expected oversized file to be refused")
	}
	writeStatic(t, filepath.Join(tree, "exact.js"), []byte("x"))
	if e := os.Truncate(filepath.Join(tree, "exact.js"), maxStaticFileSize); e != nil {
		t.Fatal(e)
	}
	f, info, _, e := openStaticFile(tree, ".", "exact.js")
	if e != nil || info.Size() != maxStaticFileSize {
		t.Fatalf("exact-limit file: info=%v err=%v", info, e)
	}
	f.Close()
}

func TestStaticFileOpenCloseLeavesNoDescriptors(t *testing.T) {
	tree, _ := staticTree(t)
	count := func() int {
		d, e := os.ReadDir("/proc/self/fd")
		if e != nil {
			t.Skip("no /proc/self/fd")
		}
		return len(d)
	}
	before := count()
	for i := 0; i < 50; i++ {
		f, _, _, e := openStaticFile(tree, ".", "assets/app.css")
		if e != nil {
			t.Fatal(e)
		}
		if _, e := io.Copy(io.Discard, f); e != nil {
			t.Fatal(e)
		}
		if e := f.Close(); e != nil {
			t.Fatal(e)
		}
		if _, _, _, e := openStaticFile(tree, ".", "link.html"); e == nil {
			t.Fatal("expected symlink refusal")
		}
	}
	if after := count(); after > before {
		t.Fatalf("descriptor leak: before=%d after=%d", before, after)
	}
}
