//go:build linux

package preview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverStaticSingleCandidate(t *testing.T) {
	for _, rel := range []string{"index.html", "public/index.html", "dist/index.html", "build/index.html"} {
		t.Run(rel, func(t *testing.T) {
			tree := t.TempDir()
			name := filepath.Join(tree, filepath.FromSlash(rel))
			if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(name, []byte("<!doctype html>"), 0600); e != nil {
				t.Fatal(e)
			}
			entry, reason, e := discoverStatic(tree)
			if e != nil {
				t.Fatal(e)
			}
			if entry != rel || reason != "" {
				t.Fatalf("entry=%q reason=%q", entry, reason)
			}
		})
	}
}

func TestDiscoverStaticUndetermined(t *testing.T) {
	cases := map[string]func(t *testing.T, tree string){
		"none": func(t *testing.T, tree string) {
			if e := os.WriteFile(filepath.Join(tree, "README.md"), []byte("x"), 0600); e != nil {
				t.Fatal(e)
			}
		},
		"multiple": func(t *testing.T, tree string) {
			for _, rel := range []string{"index.html", "dist/index.html"} {
				name := filepath.Join(tree, filepath.FromSlash(rel))
				if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(name, []byte("x"), 0600); e != nil {
					t.Fatal(e)
				}
			}
		},
		"symlink": func(t *testing.T, tree string) {
			target := filepath.Join(t.TempDir(), "real.html")
			if e := os.WriteFile(target, []byte("x"), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(target, filepath.Join(tree, "index.html")); e != nil {
				t.Fatal(e)
			}
		},
		"directory named entry": func(t *testing.T, tree string) {
			if e := os.MkdirAll(filepath.Join(tree, "index.html"), 0700); e != nil {
				t.Fatal(e)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			tree := t.TempDir()
			setup(t, tree)
			entry, reason, e := discoverStatic(tree)
			if e != nil {
				t.Fatal(e)
			}
			if entry != "" || reason == "" {
				t.Fatalf("expected undetermined with a reason, entry=%q reason=%q", entry, reason)
			}
		})
	}
}
