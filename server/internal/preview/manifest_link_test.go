//go:build linux

package preview

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A manifest entry that exists but is a link must be authoritative and invalid,
// never treated as "no manifest" and replaced by conventional discovery.
func TestManifestLinkIsAuthoritative(t *testing.T) {
	cases := []struct {
		name string
		link func(t *testing.T, tree, target string)
	}{
		{"dangling", func(t *testing.T, tree, target string) {
			if e := os.Symlink(target, filepath.Join(tree, "kaiwu-preview.json")); e != nil {
				t.Fatal(e)
			}
		}},
		{"outside", func(t *testing.T, tree, target string) {
			writeStatic(t, target, []byte(`{"version":2,"kind":"static","entry":"index.html"}`))
			if e := os.Symlink(target, filepath.Join(tree, "kaiwu-preview.json")); e != nil {
				t.Fatal(e)
			}
		}},
		{"internal valid target", func(t *testing.T, tree, _ string) {
			writeStatic(t, filepath.Join(tree, "real.json"), []byte(`{"version":2,"kind":"static","entry":"index.html"}`))
			if e := os.Symlink(filepath.Join(tree, "real.json"), filepath.Join(tree, "kaiwu-preview.json")); e != nil {
				t.Fatal(e)
			}
		}},
		{"directory", func(t *testing.T, tree, _ string) {
			if e := os.MkdirAll(filepath.Join(tree, "kaiwu-preview.json"), 0o755); e != nil {
				t.Fatal(e)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, req, tree := staticFixture(t, siteFiles())
			c.link(t, tree, filepath.Join(t.TempDir(), "missing.json"))
			v, e := m.Status(req)
			if e != nil {
				t.Fatal(e)
			}
			// index.html exists, so a wrongly-triggered discovery would offer a preview.
			if v.Kind != "" || v.State != "unavailable" {
				t.Fatalf("link manifest fell back to discovery: %#v", v)
			}
			if _, e = m.Open(req, "login"); e == nil {
				t.Fatal("link manifest opened")
			}
			if len(m.entries) != 0 {
				t.Fatal("link manifest created an entry")
			}
		})
	}
}

// A genuinely absent manifest still discovers the single conventional entry.
func TestManifestAbsentStillDiscovers(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	v, e := m.Status(req)
	if e != nil || v.Kind != "static" || v.Entry != "index.html" || v.State != "stopped" {
		t.Fatal("discovery lost", v, e)
	}
	if _, e = os.Lstat(filepath.Join(tree, "kaiwu-preview.json")); !os.IsNotExist(e) {
		t.Fatal("manifest unexpectedly present")
	}
}

// Unreadable manifests must not leak the server path through the user message.
func TestManifestErrorHidesServerPaths(t *testing.T) {
	if os.Geteuid() != 0 {
		m, _, req, tree := staticFixture(t, siteFiles())
		writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","entry":"index.html"}`))
		if e := os.Chmod(filepath.Join(tree, "kaiwu-preview.json"), 0); e != nil {
			t.Fatal(e)
		}
		v, e := m.Status(req)
		if e != nil {
			t.Fatal(e)
		}
		if v.State != "unavailable" || strings.Contains(v.Message, tree) || strings.Contains(v.Message, "permission") {
			t.Fatalf("unreadable manifest leaked: %q", v.Message)
		}
	}
	for _, body := range []string{"{", `{"version":2,"kind":"static","entry":"../escape.html"}`} {
		m, _, req, tree := staticFixture(t, siteFiles())
		writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(body))
		v, e := m.Status(req)
		if e != nil {
			t.Fatal(e)
		}
		if v.State != "unavailable" || v.Message == "" {
			t.Fatal("invalid manifest not reported", v)
		}
		if strings.Contains(v.Message, tree) {
			t.Fatalf("message leaks path: %q", v.Message)
		}
		if strings.Contains(v.Message, body) {
			t.Fatalf("message leaks raw content: %q", v.Message)
		}
	}
}

// The gateway must reject raw encoded separators before any decoding or
// splitting, while ordinary percent-encoded names keep working.
func TestStaticGatewayRejectsRawEncodedSeparators(t *testing.T) {
	files := siteFiles()
	files["资源/说明 page.html"] = "unicode"
	files["assets/name with space.css"] = "spaced"
	m, _, req, tree := staticFixture(t, files)
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","entry":"index.html"}`), 0o600)
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	for _, target := range []string{
		"assets%2fapp.css", "assets%2Fapp.css", "assets%252fapp.css", "assets%2f%2e%2e/kaiwu-preview.json",
		"%2fetc%2fpasswd", `assets%5capp.css`, "assets%5Capp.css", "assets%255capp.css",
		"..%2f..%2fetc%2fpasswd", "index.html%2f..%2f..%2fowner.json",
	} {
		t.Run(target, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://workbench.test"+prefix+"x", nil)
			escaped := strings.TrimSuffix(prefix, "/") + "/" + target
			decoded, e := url.PathUnescape(escaped)
			if e != nil {
				decoded = escaped
			}
			r.URL.Path = decoded
			r.URL.RawPath = escaped
			r.RequestURI = escaped
			w := httptest.NewRecorder()
			m.Gateway(w, r, func(hash string) bool { return hash == "login" })
			if w.Code == 200 {
				t.Fatalf("encoded separator served: %d %q", w.Code, w.Body.String())
			}
		})
	}
	// Legitimate percent-encoded and literal names still resolve.
	for _, target := range []string{"assets/name%20with%20space.css"} {
		r := httptest.NewRequest("GET", "http://workbench.test"+prefix+"x", nil)
		escaped := strings.TrimSuffix(prefix, "/") + "/" + target
		decoded, e := url.PathUnescape(escaped)
		if e != nil {
			t.Fatal(e)
		}
		r.URL.Path, r.URL.RawPath, r.RequestURI = decoded, escaped, escaped
		w := httptest.NewRecorder()
		m.Gateway(w, r, func(hash string) bool { return hash == "login" })
		if w.Code != 200 {
			t.Fatalf("legitimate asset rejected: %s %d %s", target, w.Code, w.Body.String())
		}
	}
}

// A real HTTP round trip proves the guard reads the raw request target, not a
// value the test already decoded.
func TestStaticGatewayRawRequestTargetRejectsEncodedSeparator(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","entry":"index.html"}`), 0o600)
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.Gateway(w, r, func(hash string) bool { return hash == "login" })
	}))
	defer srv.Close()
	get := func(target string) (int, string) {
		res, e := srv.Client().Get(srv.URL + target)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	prefix := opened.URL[strings.Index(opened.URL, "/preview/"):]
	prefix = prefix[:strings.LastIndex(prefix, "/")+1]
	// A client may spell the route prefix itself with percent-encoding
	// ("/pre%76iew/"), which decodes to the same URL.Path. The guard must still
	// see the encoded separator that follows it.
	encodedPrefix := "/pre%76iew/" + strings.TrimPrefix(prefix, "/preview/")
	if code, body := get(encodedPrefix + "assets%2fapp.css"); code == 200 || body == "body{color:red}" {
		t.Fatalf("encoded route prefix bypassed guard: %d %q", code, body)
	}
	if code, body := get(encodedPrefix + "assets%5capp.css"); code == 200 || body == "body{color:red}" {
		t.Fatalf("encoded route prefix bypassed backslash guard: %d %q", code, body)
	}
	if code, body := get(encodedPrefix + "assets/app.css"); code != 200 || body != "body{color:red}" {
		t.Fatalf("encoded route prefix broke legitimate asset: %d %q", code, body)
	}
	if code, body := get(prefix + "assets%2fapp.css"); code == 200 || body == "body{color:red}" {
		t.Fatalf("raw encoded separator served: %d %q", code, body)
	}
	if code, body := get(prefix + "assets/app.css"); code != 200 || body != "body{color:red}" {
		t.Fatalf("legitimate asset rejected: %d %q", code, body)
	}
}
