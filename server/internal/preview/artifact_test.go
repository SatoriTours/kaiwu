//go:build linux

package preview

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// artifactFixture writes a v2 manifest that declares explicit downloadable
// files next to (or instead of) a web entry, and refreshes the fingerprint.
func artifactFixture(t *testing.T, m *Manager, req Request, tree string, manifest string, files map[string]string) Request {
	t.Helper()
	for name, body := range files {
		writeStatic(t, filepath.Join(tree, filepath.FromSlash(name)), []byte(body))
	}
	writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(manifest))
	shown, e := m.Status(req)
	if e != nil {
		t.Fatal(e)
	}
	req.ManifestID = shown.ManifestID
	return req
}

func TestExplicitArtifactDownloadsAreAttachmentBound(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"static","root":".","entry":"index.html","path":"/",
		"artifacts":[{"path":"reports/summary.pdf","name":"总结 报告.pdf"},{"path":"logs/build.txt","type":"text/plain; charset=utf-8"}]}`,
		map[string]string{"reports/summary.pdf": "%PDF-1.4 real bytes", "logs/build.txt": "build ok"})

	shown, e := m.Status(req)
	if e != nil || shown.Kind != "static" || shown.State != "stopped" {
		t.Fatal(shown, e)
	}
	if len(shown.Artifacts) != 2 || shown.Artifacts[0].Name != "总结 报告.pdf" || shown.Artifacts[0].Type != "application/pdf" ||
		shown.Artifacts[0].Size != int64(len("%PDF-1.4 real bytes")) || len(shown.Artifacts[0].ID) != 64 || shown.Artifacts[0].URL != "" {
		t.Fatalf("status artifacts %#v", shown.Artifacts)
	}
	if shown.Artifacts[1].Name != "build.txt" || shown.Artifacts[1].Type != "text/plain; charset=utf-8" {
		t.Fatalf("named artifact %#v", shown.Artifacts[1])
	}
	opened, e := m.Open(req, "login")
	if e != nil || !strings.HasPrefix(opened.URL, "/preview/") {
		t.Fatal(opened, e)
	}
	if len(opened.Artifacts) != 2 || !strings.HasSuffix(opened.Artifacts[0].URL, "/~download/"+opened.Artifacts[0].ID) ||
		!strings.HasPrefix(opened.Artifacts[0].URL, "/preview/") {
		t.Fatalf("open artifacts %#v", opened.Artifacts)
	}
	// The web entry still loads from the same capability.
	if w := m.callGateway(opened.URL, "GET", true); w.Code != 200 || w.Body.String() != siteFiles()["index.html"] {
		t.Fatal("entry", w.Code, w.Body.String())
	}
	w := m.callGateway(opened.Artifacts[0].URL, "GET", true)
	if w.Code != 200 || w.Body.String() != "%PDF-1.4 real bytes" {
		t.Fatal("download", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") ||
		!strings.Contains(got, `filename="`) || !strings.Contains(got, "filename*=UTF-8''"+url.PathEscape("总结 报告.pdf")) {
		t.Fatal("disposition", got)
	}
	if w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("headers", w.Header())
	}
	if w.Header().Get("Content-Length") != "19" {
		t.Fatal("length", w.Header())
	}
	h := m.callGateway(opened.Artifacts[0].URL, "HEAD", true)
	if h.Code != 200 || h.Body.Len() != 0 || h.Header().Get("Content-Length") != "19" ||
		!strings.HasPrefix(h.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("head", h.Code, h.Body.Len(), h.Header())
	}
	if p := m.callGateway(opened.Artifacts[0].URL, "POST", true); p.Code != 405 {
		t.Fatal("POST", p.Code)
	}
}

func TestFilesOnlyManifestHasNoFakePage(t *testing.T) {
	m, _, req, tree := staticFixture(t, map[string]string{})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files",
		"artifacts":[{"path":"out/report.csv"}]}`, map[string]string{"out/report.csv": "a,b\n1,2\n"})

	shown, e := m.Status(req)
	if e != nil || shown.Kind != "files" || shown.URL != "" || shown.Path != "" || shown.Entry != "" {
		t.Fatal(shown, e)
	}
	if len(shown.Artifacts) != 1 || shown.Artifacts[0].Type != "text/csv; charset=utf-8" {
		t.Fatalf("files artifacts %#v", shown.Artifacts)
	}
	opened, e := m.Open(req, "login")
	if e != nil || opened.URL != "" || len(opened.Artifacts) != 1 {
		t.Fatal("files open", opened, e)
	}
	if w := m.callGateway(opened.Artifacts[0].URL, "GET", true); w.Code != 200 || w.Body.String() != "a,b\n1,2\n" {
		t.Fatal("files download", w.Code, w.Body.String())
	}
	m.mu.Lock()
	en := m.entries[req.TaskID]
	guard := en == nil || en.kind != "files" || en.cmd != nil || en.process != nil || en.lease != nil || en.port != 0
	m.mu.Unlock()
	if guard {
		t.Fatal("files-only preview used a process/port/lease")
	}
	prefix := opened.Artifacts[0].URL[:strings.Index(opened.Artifacts[0].URL, "/~download/")+1]
	for _, path := range []string{prefix, prefix + "index.html"} {
		if w := m.callGateway(path, "GET", true); w.Code == 200 {
			t.Fatalf("files-only served a page: %s", path)
		}
	}
	// A files-only manifest without artifacts is invalid, and one with an entry
	// may not claim to be web-less.
	for _, bad := range []string{
		`{"version":2,"kind":"files"}`,
		`{"version":2,"kind":"files","entry":"index.html","artifacts":[{"path":"out/report.csv"}]}`,
		`{"version":1,"command":["x","{port}"],"path":"/","artifacts":[{"path":"out/report.csv"}]}`,
	} {
		writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(bad))
		v, e := m.Status(req)
		if e != nil || v.Kind != "" || v.State != "unavailable" {
			t.Fatalf("%s: %#v %v", bad, v, e)
		}
	}
}

func TestArtifactManifestRejectsUnsafeDeclarations(t *testing.T) {
	outside := t.TempDir()
	writeStatic(t, filepath.Join(outside, "secret.pdf"), []byte("outside"))
	for _, tc := range []struct{ name, manifest string }{
		{"traversal", `{"version":2,"kind":"files","artifacts":[{"path":"../secret.pdf"}]}`},
		{"absolute", `{"version":2,"kind":"files","artifacts":[{"path":"/etc/passwd"}]}`},
		{"hidden", `{"version":2,"kind":"files","artifacts":[{"path":".env"}]}`},
		{"private", `{"version":2,"kind":"files","artifacts":[{"path":"keys/id.pem"}]}`},
		{"encoded", `{"version":2,"kind":"files","artifacts":[{"path":"out%2freport.csv"}]}`},
		{"backslash", `{"version":2,"kind":"files","artifacts":[{"path":"out\\report.csv"}]}`},
		{"symlink", `{"version":2,"kind":"files","artifacts":[{"path":"link.pdf"}]}`},
		{"fifo", `{"version":2,"kind":"files","artifacts":[{"path":"pipe.pdf"}]}`},
		{"directory", `{"version":2,"kind":"files","artifacts":[{"path":"out"}]}`},
		{"oversize", `{"version":2,"kind":"files","artifacts":[{"path":"big.pdf"}]}`},
		{"unknown-type", `{"version":2,"kind":"files","artifacts":[{"path":"out/report.exe"}]}`},
		{"forged-type", `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv","type":"text/html; charset=utf-8"}]}`},
		{"name-separator", `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv","name":"a/b.csv"}]}`},
		{"name-control", `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv","name":"a\r\nb.csv"}]}`},
		{"too-many", `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv"},{"path":"out/report.csv"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, req, tree := staticFixture(t, map[string]string{"out/report.csv": "a"})
			writeStatic(t, filepath.Join(outside, "big.pdf"), []byte("x"))
			if e := os.Truncate(filepath.Join(outside, "big.pdf"), maxStaticFileSize+1); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(filepath.Join(outside, "secret.pdf"), filepath.Join(tree, "link.pdf")); e != nil {
				t.Fatal(e)
			}
			if e := unix.Mkfifo(filepath.Join(tree, "pipe.pdf"), 0o644); e != nil {
				t.Fatal(e)
			}
			_ = os.MkdirAll(filepath.Join(tree, "out"), 0o755)
			if tc.name == "oversize" {
				writeStatic(t, filepath.Join(tree, "big.pdf"), nil)
				if e := os.Truncate(filepath.Join(tree, "big.pdf"), maxStaticFileSize+1); e != nil {
					t.Fatal(e)
				}
			}
			if tc.name == "too-many" {
				decl := `{"path":"out/report.csv"}`
				tc.manifest = `{"version":2,"kind":"files","artifacts":[` + strings.TrimSuffix(strings.Repeat(decl+",", 17), ",") + `]}`
			}
			writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(tc.manifest))
			v, e := m.Status(req)
			if e != nil || v.Kind != "" || v.State != "unavailable" || len(v.Artifacts) != 0 {
				t.Fatalf("%#v %v", v, e)
			}
			if v.Message == "" || strings.Contains(v.Message, tree) || strings.Contains(v.Message, outside) {
				t.Fatal("message leaked a path or is empty", v.Message)
			}
			if _, e = m.Open(req, "login"); e == nil {
				t.Fatal("unsafe manifest opened")
			}
			if len(m.entries) != 0 {
				t.Fatal("unsafe manifest created an entry")
			}
		})
	}
	// A declared file that disappears after the manifest was accepted is refused
	// at download time rather than 200 with nothing.
	m, _, req, tree := staticFixture(t, map[string]string{"out/report.csv": "a"})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv"}]}`, nil)
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(tree, "out/report.csv")); e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(opened.Artifacts[0].URL, "GET", true); w.Code == 200 {
		t.Fatal("missing declared file served", w.Code)
	}
}

func TestArtifactDownloadRefusesUndeclaredAndChanged(t *testing.T) {
	m, _, req, tree := staticFixture(t, map[string]string{"out/report.csv": "a"})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv"}]}`, nil)
	opened, e := m.Open(req, "login")
	if e != nil || len(opened.Artifacts) != 1 {
		t.Fatal(opened, e)
	}
	good := opened.Artifacts[0].URL
	prefix := good[:strings.Index(good, "/~download/")+1]
	for _, bad := range []string{
		prefix + "~download/" + strings.Repeat("0", 64),
		prefix + "~download/",
		prefix + "~download/../../owner.json",
		prefix + "~download/%2e%2e/owner.json",
		prefix + "~download/out/report.csv",
		prefix + "~download/" + opened.Artifacts[0].ID + "/extra",
	} {
		if w := m.callGateway(bad, "GET", true); w.Code == 200 {
			t.Fatalf("undeclared download served: %s", bad)
		}
	}
	// Revoked login refuses before any bytes are read.
	if w := m.callGateway(good, "GET", false); w.Code != 401 {
		t.Fatal("revoked login", w.Code)
	}
	// A changed manifest invalidates the old capability, not just the ID.
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"other.csv"}]}`,
		map[string]string{"other.csv": "b"})
	if w := m.callGateway(good, "GET", true); w.Code == 200 {
		t.Fatal("stale manifest capability served", w.Code)
	}
	// Stop invalidates every capability for the task.
	if _, e := m.Open(req, "login"); e != nil {
		t.Fatal(e)
	}
	req.OperationID = "stop-artifacts"
	if _, e := m.Stop(req); e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(good, "GET", true); w.Code == 200 {
		t.Fatal("stopped capability served", w.Code)
	}
}

func TestArtifactDownloadsNeverStartProcesses(t *testing.T) {
	// The static fixture already fails the test if a writer lease is reserved.
	m, _, req, tree := staticFixture(t, map[string]string{"out/report.csv": "a"})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv"}]}`, nil)
	shown, e := m.Status(req)
	if e != nil || len(shown.Artifacts) != 1 {
		t.Fatal(shown, e)
	}
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(opened.Artifacts[0].URL, "GET", true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	en := m.entries[req.TaskID]
	if en == nil || en.cmd != nil || en.process != nil || en.lease != nil || en.port != 0 {
		t.Fatal("artifact download started a process")
	}
	for _, key := range m.tokens {
		if key.entry != en {
			t.Fatal("capability bound to another entry")
		}
	}
}

// TestArtifactReservedSegmentCannotShadowAsset keeps the reserved download
// segment from ever addressing a normal static asset.
func TestArtifactReservedSegmentCannotShadowAsset(t *testing.T) {
	files := siteFiles()
	files["~download/thing.css"] = "body{}"
	m, _, req, _ := staticFixture(t, files)
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	if w := m.callGateway(prefix+"~download/thing.css", "GET", true); w.Code == 200 {
		t.Fatal("reserved segment served a static asset", w.Code)
	}
}

// TestDownloadResolvesOnlyInsideApprovedSnapshot pins the invariant that a
// download may not re-read the manifest: the very same declaration ID must be
// refused when the approved snapshot does not contain it, even though the file
// on disk does. Re-reading the manifest here is exactly what made an already
// issued capability able to serve a later declaration.
func TestDownloadResolvesOnlyInsideApprovedSnapshot(t *testing.T) {
	m, _, req, tree := staticFixture(t, map[string]string{"out/first.txt": "first-body"})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"out/first.txt"}]}`, nil)
	shown, e := m.Status(req)
	if e != nil || len(shown.Artifacts) != 1 {
		t.Fatal(shown, e)
	}
	v, _, _, snap, ok, e := m.describeSnapshot(req)
	if e != nil || !ok || len(v.Artifacts) != 1 {
		t.Fatal(v, ok, e)
	}
	if e := os.WriteFile(filepath.Join(tree, "out", "second.txt"), []byte("second-body"), 0o600); e != nil {
		t.Fatal(e)
	}
	next := `{"version":2,"kind":"files","artifacts":[{"path":"out/second.txt"}]}`
	writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"), []byte(next))
	if _, e := resolveArtifactIn(tree, snap, artifactID("out/second.txt")); e == nil {
		t.Fatal("resolver served a declaration outside the approved snapshot")
	}
	if a, e := resolveArtifactIn(tree, snap, artifactID("out/first.txt")); e != nil || a.Name != "first.txt" {
		t.Fatalf("approved declaration unavailable: %#v %v", a, e)
	}
}

// TestSameArtifactIDMetadataChangeRebindsCapability covers a manifest change
// that keeps the declared path (and therefore the ID) but changes the download
// name: the old capability must still be refused, and only a fresh one may show
// the new metadata.
func TestSameArtifactIDMetadataChangeRebindsCapability(t *testing.T) {
	m, _, req, tree := staticFixture(t, map[string]string{"out/report.csv": "a,b\n"})
	req = artifactFixture(t, m, req, tree, `{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv","name":"旧名.csv"}]}`, nil)
	opened, e := m.Open(req, "login")
	if e != nil || len(opened.Artifacts) != 1 || opened.Artifacts[0].Name != "旧名.csv" {
		t.Fatal(opened, e)
	}
	oldURL, oldID := opened.Artifacts[0].URL, opened.Artifacts[0].ID
	writeStatic(t, filepath.Join(tree, "kaiwu-preview.json"),
		[]byte(`{"version":2,"kind":"files","artifacts":[{"path":"out/report.csv","name":"新名.csv"}]}`))

	if w := m.callGateway(oldURL, "GET", true); w.Code != 409 {
		t.Fatalf("stale capability served changed metadata: %d %s", w.Code, w.Body.String())
	}
	fresh, e := m.Open(req, "login")
	if e != nil || len(fresh.Artifacts) != 1 || fresh.Artifacts[0].ID != oldID || fresh.Artifacts[0].Name != "新名.csv" {
		t.Fatalf("fresh capability %#v %v", fresh, e)
	}
	w := m.callGateway(fresh.Artifacts[0].URL, "GET", true)
	if w.Code != 200 || w.Body.String() != "a,b\n" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "filename*=UTF-8''%E6%96%B0%E5%90%8D.csv") {
		t.Fatalf("fresh capability download %d %s %s", w.Code, w.Body.String(), w.Header().Get("Content-Disposition"))
	}
}
