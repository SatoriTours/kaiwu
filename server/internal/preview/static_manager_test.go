//go:build linux

package preview

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// staticFixture reuses the existing real worktree/store fixture and only adds
// static files, so v1 process tests and static tests share one harness.
func staticFixture(t *testing.T, files map[string]string) (*Manager, *workbench.Store, Request, string) {
	t.Helper()
	m, s, req, tree := fixture(t, "missing")
	m.cfg.Reserve = func(context.Context, string) (Lease, error) {
		t.Error("static preview must not reserve a writer lease")
		return nil, os.ErrInvalid
	}
	for name, body := range files {
		writeStatic(t, filepath.Join(tree, filepath.FromSlash(name)), []byte(body))
	}
	// fixture's Status ran before the files existed; static discovery is
	// stateless, so the caller re-reads Status for the fingerprint.
	return m, s, req, tree
}

func (m *Manager) callGateway(url, method string, valid bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://workbench.test"+url, nil)
	r.Header.Set("Cookie", "kaiwu_session=secret")
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	m.Gateway(w, r, func(hash string) bool { return valid && hash == "login" })
	return w
}

func siteFiles() map[string]string {
	return map[string]string{
		"index.html":     `<!doctype html><link rel="stylesheet" href="assets/app.css"><script src="assets/app.js"></script><img src="img/pic.png">`,
		"assets/app.css": "body{color:red}",
		"assets/app.js":  "console.log(1)",
		"img/pic.png":    "\x89PNG\r\n\x1a\nxxxx",
	}
}

func TestStaticPreviewServesEntryAndRelativeAssetsWithoutStart(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	shown, e := m.Status(req)
	if e != nil || shown.Kind != "static" || shown.State != "stopped" || shown.Entry != "index.html" || shown.Root != "." {
		t.Fatal(shown, e)
	}
	if len(shown.ManifestID) != 64 {
		t.Fatalf("static fingerprint not deterministic hex: %q", shown.ManifestID)
	}
	again, e := m.Status(req)
	if e != nil || again.ManifestID != shown.ManifestID {
		t.Fatal("fingerprint unstable", again, e)
	}
	opened, e := m.Open(req, "login")
	if e != nil || opened.State != "running" || !strings.HasPrefix(opened.URL, "/preview/") || !strings.HasSuffix(opened.URL, "/index.html") {
		t.Fatal(opened, e)
	}
	m.mu.Lock()
	en := m.entries[req.TaskID]
	hasProcess := en == nil || en.cmd != nil || en.process != nil || en.lease != nil || en.port != 0 || en.kind != "static" || en.tree != tree
	m.mu.Unlock()
	if hasProcess {
		t.Fatal("static entry used a process/port/lease")
	}
	w := m.callGateway(opened.URL, "GET", true)
	if w.Code != 200 || w.Body.String() != siteFiles()["index.html"] || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatal("csp", csp)
	}
	for _, h := range []string{"Set-Cookie", "Access-Control-Allow-Origin", "Location"} {
		if w.Header().Get(h) != "" {
			t.Fatal("static leaked header", h)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("security headers", w.Header())
	}
	if h := m.callGateway(opened.URL, "HEAD", true); h.Code != 200 || h.Body.Len() != 0 || h.Header().Get("Content-Length") != strconv.Itoa(len(siteFiles()["index.html"])) {
		t.Fatal("HEAD", h.Code, h.Body.Len(), h.Header())
	}
	if p := m.callGateway(opened.URL, "POST", true); p.Code != 405 {
		t.Fatal("POST accepted", p.Code)
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	for rel, want := range map[string]string{"assets/app.css": "text/css; charset=utf-8", "assets/app.js": "text/javascript; charset=utf-8", "img/pic.png": "image/png"} {
		r := m.callGateway(prefix+rel, "GET", true)
		if r.Code != 200 || r.Header().Get("Content-Type") != want || r.Body.String() != siteFiles()[rel] {
			t.Fatal(rel, r.Code, r.Body.String(), r.Header())
		}
	}
	// Reopening reuses the same static entry and issues a second short-lived cap.
	second, e := m.Open(req, "login")
	if e != nil || second.URL == opened.URL {
		t.Fatal("second cap", second, e)
	}
	m.mu.Lock()
	reused := m.entries[req.TaskID]
	n := len(m.entries)
	m.mu.Unlock()
	if reused != en || n != 1 {
		t.Fatal("reopen created a new entry")
	}
	if w = m.callGateway(second.URL, "GET", true); w.Code != 200 {
		t.Fatal("second cap", w.Code)
	}
}

func TestStaticDiscoveryCasesNeverFallBackFromInvalidManifest(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	_ = os.WriteFile(filepath.Join(tree, "dist/index.html"), nil, 0o644)
	writeStatic(t, filepath.Join(tree, "dist/index.html"), []byte("dist"))
	v, e := m.Status(req)
	if e != nil || v.Kind != "" || v.State != "unavailable" || !strings.Contains(v.Message, "多个 HTML 入口") {
		t.Fatal("multiple candidates", v, e)
	}
	if _, e = m.Open(req, "login"); e == nil {
		t.Fatal("ambiguous static opened")
	}
	for _, name := range []string{"dist/index.html", "dist", "assets/app.css"} {
		_ = os.Remove(filepath.Join(tree, name))
	}
	_ = os.Remove(filepath.Join(tree, "index.html"))
	v, e = m.Status(req)
	if e != nil || v.State != "unavailable" || !strings.Contains(v.Message, "没有可确定的 HTML 入口") {
		t.Fatal("zero candidates", v, e)
	}
	// An existing invalid manifest is authoritative: no silent discovery.
	writeStatic(t, filepath.Join(tree, "index.html"), []byte("html"))
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","entry":"../escape.html"}`), 0o600)
	v, e = m.Status(req)
	if e != nil || v.Kind != "" || v.State != "unavailable" {
		t.Fatal("invalid manifest fell back to discovery", v, e)
	}
	if _, e = m.Open(req, "login"); e == nil {
		t.Fatal("invalid manifest opened")
	}
	if len(m.entries) != 0 {
		t.Fatal("invalid static created an entry")
	}
}

func TestStaticManifestRootEntryAndUnsafeEntry(t *testing.T) {
	files := map[string]string{"public/index.html": "public", "public/app.css": "css", "public/leak.env": "x"}
	m, _, req, tree := staticFixture(t, files)
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","root":"public","entry":"index.html","path":"/"}`), 0o600)
	v, e := m.Status(req)
	if e != nil || v.Kind != "static" || v.Root != "public" || v.Entry != "index.html" || !strings.HasPrefix(v.Path, "/") {
		t.Fatal(v, e)
	}
	opened, e := m.Open(req, "login")
	if e != nil || !strings.HasSuffix(opened.URL, "/index.html") {
		t.Fatal(opened, e)
	}
	if w := m.callGateway(opened.URL, "GET", true); w.Code != 200 || w.Body.String() != "public" {
		t.Fatal(w.Code, w.Body.String())
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	if w := m.callGateway(prefix+"app.css", "GET", true); w.Code != 200 || w.Body.String() != "css" {
		t.Fatal("root-relative asset", w.Code, w.Body.String())
	}
	if w := m.callGateway(prefix+"leak.env", "GET", true); w.Code == 200 {
		t.Fatal("private asset served")
	}
	// Entry that cannot be opened safely makes the preview unavailable.
	_ = os.Remove(filepath.Join(tree, "public/index.html"))
	v, e = m.Status(req)
	if e != nil || v.State != "unavailable" || v.Kind != "" {
		t.Fatal("unsafe entry shown usable", v, e)
	}
}

func TestStaticStartHasNoProcessAndStopInvalidates(t *testing.T) {
	m, _, req, _ := staticFixture(t, siteFiles())
	shown, e := m.Status(req)
	if e != nil {
		t.Fatal(e)
	}
	req.ManifestID = shown.ManifestID
	v, e := m.Start(req)
	if e != nil || v.State != "running" || v.Kind != "static" {
		t.Fatal(v, e)
	}
	if v, e = m.Start(req); e != nil || v.State != "running" {
		t.Fatal("repeat start", v, e)
	}
	m.mu.Lock()
	en := m.entries[req.TaskID]
	guard := en == nil || en.cmd != nil || en.lease != nil
	m.mu.Unlock()
	if guard {
		t.Fatal("static start started a process")
	}
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(opened.URL, "GET", true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	req.OperationID = "stop-static"
	if v, e = m.Stop(req); e != nil || v.State != "stopped" {
		t.Fatal(v, e)
	}
	if w := m.callGateway(opened.URL, "GET", true); w.Code == 200 || w.Body.String() == siteFiles()["index.html"] {
		t.Fatal("stopped cap still served", w.Code)
	}
	if v, e = m.Status(req); e != nil || v.State != "stopped" {
		t.Fatal("static status after stop", v, e)
	}
}

func TestStaticGatewayRejectsPathAttacksAndLeaksNoPath(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	outside := t.TempDir()
	writeStatic(t, filepath.Join(outside, "secret.html"), []byte("outside"))
	if e := os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(tree, "link.html")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(tree, "linkdir")); e != nil {
		t.Fatal(e)
	}
	if e := unix.Mkfifo(filepath.Join(tree, "pipe.css"), 0o644); e != nil {
		t.Fatal(e)
	}
	writeStatic(t, filepath.Join(tree, ".env"), []byte("SECRET=1"))
	writeStatic(t, filepath.Join(tree, "main.go"), []byte("package main"))
	writeStatic(t, filepath.Join(tree, "secret.pem"), []byte("key"))
	writeStatic(t, filepath.Join(tree, "node_modules/x.js"), []byte("x"))
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	prefix := opened.URL[:strings.LastIndex(opened.URL, "/")+1]
	for _, rel := range []string{
		"../kaiwu-preview.json", "%2e%2e/kaiwu-preview.json", "..%2fowner.json", "assets/../../owner.json",
		"%2fetc%2fpasswd", "//etc/passwd", "/etc/passwd", `assets\app.css`, ".env", ".git",
		"main.go", "secret.pem", "node_modules/x.js", "link.html", "linkdir/secret.html", "pipe.css",
		"assets", "assets/", "index.html%00.png", "index.html%0a",
	} {
		done := make(chan *httptest.ResponseRecorder, 1)
		go func(rel string) { done <- m.callGateway(prefix+rel, "GET", true) }(rel)
		select {
		case w := <-done:
			if w.Code == 200 || w.Body.String() == "outside" || w.Body.String() == "SECRET=1" {
				t.Fatal("attack served", rel, w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), tree) || strings.Contains(w.Body.String(), outside) {
				t.Fatal("error leaked path", rel, w.Body.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("static gateway blocked on", rel)
		}
	}
	// Directory listing must never be produced.
	if w := m.callGateway(prefix, "GET", true); w.Code == 200 && strings.Contains(w.Body.String(), "index.html") && w.Body.String() != siteFiles()["index.html"] {
		t.Fatal("directory listing leaked", w.Body.String())
	}
}

func TestStaticCapabilityInvalidation(t *testing.T) {
	m, s, req, tree := staticFixture(t, siteFiles())
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(opened.URL, "GET", false); w.Code != 401 {
		t.Fatal("logout", w.Code)
	}
	if w := m.callGateway("/preview/"+strings.Repeat("b", 64)+"/index.html", "GET", true); w.Code != 401 {
		t.Fatal("unknown cap", w.Code)
	}
	// Round change invalidates.
	_ = s.Update(func(st *workbench.State) error {
		c := st.Conversations["c"]
		c.Rounds = append(c.Rounds, workbench.Round{ID: "r2", Phase: "finished", FinishedAt: "later"})
		st.Conversations["c"] = c
		return nil
	})
	if w := m.callGateway(opened.URL, "GET", true); w.Code == 200 {
		t.Fatal("stale round served")
	}
	req.RoundID = "r2"
	fresh, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(fresh, e)
	}
	if w := m.callGateway(fresh.URL, "GET", true); w.Code != 200 {
		t.Fatal("current round", w.Code)
	}
	if w := m.callGateway(opened.URL, "GET", true); w.Code == 200 {
		t.Fatal("historical cap resumed")
	}
	// Manifest change invalidates the current entries and caps.
	_ = os.WriteFile(filepath.Join(tree, "kaiwu-preview.json"), []byte(`{"version":2,"kind":"static","entry":"index.html"}`), 0o600)
	writeStatic(t, filepath.Join(tree, "index.html"), []byte("changed"))
	if w := m.callGateway(fresh.URL, "GET", true); w.Code == 200 {
		t.Fatal("manifest change kept cap")
	}
	changed, e := m.Open(req, "login")
	if e != nil || changed.ManifestID == fresh.ManifestID {
		t.Fatal("manifest change not detected", changed, e)
	}
	if w := m.callGateway(changed.URL, "GET", true); w.Code != 200 || w.Body.String() != "changed" {
		t.Fatal("new manifest", w.Code, w.Body.String())
	}
	// Expiry and Close invalidate without touching disk state.
	m.mu.Lock()
	for _, token := range m.tokens {
		token.expires = time.Now().Add(-time.Second)
	}
	m.mu.Unlock()
	if w := m.callGateway(changed.URL, "GET", true); w.Code != 401 {
		t.Fatal("expired cap", w.Code)
	}
	live, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(live.URL, "GET", true); w.Code == 200 {
		t.Fatal("closed manager served")
	}
	if _, e = m.Open(req, "login"); e == nil {
		t.Fatal("closed manager opened")
	}
}

func TestStaticReadsAreBoundedBySingleFileLimit(t *testing.T) {
	m, _, req, tree := staticFixture(t, map[string]string{"index.html": "html"})
	big := filepath.Join(tree, "big.js")
	writeStatic(t, big, []byte("x"))
	if e := os.Truncate(big, maxStaticFileSize+1); e != nil {
		t.Fatal(e)
	}
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(opened.URL[:strings.LastIndex(opened.URL, "/")+1]+"big.js", "GET", true); w.Code == 200 {
		t.Fatal("oversized asset served")
	}
	f, _, mime, e := openStaticFile(tree, ".", "index.html")
	if e != nil || mime != "text/html; charset=utf-8" {
		t.Fatal(mime, e)
	}
	f.Close()
}

func TestStaticSegmentsRejectAllControlCharacters(t *testing.T) {
	tree, _ := staticTree(t)
	for _, name := range []string{
		"index\x00.html", "index\x01.html", "index\x09.html", "index\x0b.html", "index\x1f.html",
		"index\x7f.html", "index\u0085.html", "index\u202e.html", "index\u0000.html",
		"assets/app\x00.css", "assets/app\x1b.css",
	} {
		if _, ok := staticSegments(name, false); ok {
			t.Fatal("control accepted in asset", strconv.Quote(name))
		}
		if f, _, _, e := openStaticFile(tree, ".", name); e == nil {
			f.Close()
			t.Fatal("control asset opened", strconv.Quote(name))
		}
		if root, ok := staticSegments(name, true); ok && root != nil {
			t.Fatal("control accepted in root", strconv.Quote(name))
		}
		if f, _, _, e := openStaticFile(tree, name, "index.html"); e == nil {
			f.Close()
			t.Fatal("control root opened", strconv.Quote(name))
		}
	}
	if f, _, _, e := openStaticFile(tree, ".", "index.html"); e != nil {
		f.Close()
		t.Fatal("plain html refused", e)
	} else {
		io.Copy(io.Discard, f)
		f.Close()
	}
}

// A) CSP must name a real same-site origin for every asset type, built from a
// strictly validated Host, never from an unvalidated header value.
func TestStaticCSPUsesValidatedOriginAndCapPrefix(t *testing.T) {
	m, _, req, _ := staticFixture(t, siteFiles())
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "http://workbench.test"+opened.URL, nil)
	w := httptest.NewRecorder()
	m.Gateway(w, r, func(hash string) bool { return hash == "login" })
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	cap := "http://workbench.test/preview/" + opened.URL[len("/preview/"):strings.Index(opened.URL[len("/preview/"):], "/")+len("/preview/")] + "/"
	for _, want := range []string{
		"script-src 'unsafe-inline' 'unsafe-eval' " + cap,
		"style-src 'unsafe-inline' " + cap,
		"img-src data: blob: " + cap,
		"font-src data: " + cap,
		"media-src data: " + cap,
		"connect-src " + cap,
		"sandbox allow-scripts",
		"frame-ancestors 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("missing %q in %q", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "connect-src 'none'") || strings.Contains(csp, "style-src 'unsafe-inline';") {
		t.Fatalf("unsafe or broken csp: %q", csp)
	}
	// A Host that cannot be a CSP source is refused outright instead of being
	// interpolated into the policy.
	for _, host := range []string{"evil host", "workbench.test; script-src *", "workbench.test/../../x", "workbench.test\nX: y", "workbench.test'script-src"} {
		bad := httptest.NewRequest("GET", "http://workbench.test"+opened.URL, nil)
		bad.Host = host
		bw := httptest.NewRecorder()
		m.Gateway(bw, bad, func(hash string) bool { return hash == "login" })
		if bw.Code == 200 {
			t.Fatalf("unsafe host %q served with csp %q", host, bw.Header().Get("Content-Security-Policy"))
		}
	}
	// IPv6 and explicit ports are legitimate hosts and stay usable.
	for _, host := range []string{"127.0.0.1:7331", "[::1]:7331"} {
		ok := httptest.NewRequest("GET", "http://workbench.test"+opened.URL, nil)
		ok.Host = host
		ow := httptest.NewRecorder()
		m.Gateway(ow, ok, func(hash string) bool { return hash == "login" })
		if ow.Code != 200 || !strings.Contains(ow.Header().Get("Content-Security-Policy"), "http://"+host+"/preview/") {
			t.Fatalf("host %q: %d %q", host, ow.Code, ow.Header().Get("Content-Security-Policy"))
		}
	}
}

// B) Stop/StopTask must not leave a dead entry that blocks reopening the same
// configuration, and old capabilities must never become valid again.
func TestStaticReopensAfterStopAndStopTask(t *testing.T) {
	m, _, req, _ := staticFixture(t, siteFiles())
	first, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	stop := req
	stop.OperationID = "stop-static-reopen"
	if v, e := m.Stop(stop); e != nil || v.State != "stopped" {
		t.Fatal(v, e)
	}
	reopened, e := m.Open(req, "login")
	if e != nil || reopened.State != "running" || reopened.URL == first.URL {
		t.Fatal("reopen after stop", reopened, e)
	}
	if w := m.callGateway(first.URL, "GET", true); w.Code == 200 {
		t.Fatal("pre-stop cap revived")
	}
	if w := m.callGateway(reopened.URL, "GET", true); w.Code != 200 {
		t.Fatal("reopened cap", w.Code)
	}
	if e := m.StopTask(context.Background(), req.TaskID); e != nil {
		t.Fatal(e)
	}
	if w := m.callGateway(reopened.URL, "GET", true); w.Code == 200 {
		t.Fatal("StopTask kept cap alive")
	}
	after, e := m.Open(req, "login")
	if e != nil || after.State != "running" || after.URL == reopened.URL {
		t.Fatal("reopen after StopTask", after, e)
	}
	if w := m.callGateway(after.URL, "GET", true); w.Code != 200 {
		t.Fatal("cap after StopTask", w.Code)
	}
	m.mu.Lock()
	en := m.entries[req.TaskID]
	bad := en == nil || en.kind != "static" || en.cmd != nil || en.process != nil || en.lease != nil || en.port != 0 || len(m.entries) != 1
	m.mu.Unlock()
	if bad {
		t.Fatal("reopen used a process or leaked entries")
	}
	// Start over the same configuration also recovers a stopped static entry.
	restart := req
	restart.OperationID = "restart-static"
	shown, e := m.Status(restart)
	if e != nil {
		t.Fatal(e)
	}
	restart.ManifestID = shown.ManifestID
	if v, e := m.Start(restart); e != nil || v.State != "running" {
		t.Fatal("restart static", v, e)
	}
}

type failingLease struct{}

func (failingLease) File() *os.File     { return nil }
func (failingLease) Attempt() error     { return nil }
func (failingLease) Started(int) error  { return nil }
func (failingLease) Release(bool) error { return errors.New("lease release failed") }

// C) Replacing an unreclaimed entry must fail loudly and keep the original
// entry tracked, instead of silently dropping the lease/process ownership.
func TestStaticReplacementPropagatesStopFailure(t *testing.T) {
	m, _, req, tree := staticFixture(t, siteFiles())
	dead := exec.Command("true")
	if e := dead.Run(); e != nil {
		t.Fatal(e)
	}
	old := &entry{req: req, tree: tree, view: base(req, "running", "old"), lease: failingLease{}, cmd: dead, done: make(chan struct{})}
	close(old.done)
	m.mu.Lock()
	m.entries[req.TaskID] = old
	m.mu.Unlock()
	if _, e := m.Open(req, "login"); e == nil {
		t.Fatal("stop failure ignored")
	}
	m.mu.Lock()
	kept := m.entries[req.TaskID] == old
	n := len(m.tokens)
	m.mu.Unlock()
	if !kept || n != 0 {
		t.Fatal("failed stop lost entry tracking or issued a cap", kept, n)
	}
	if en := old; en == nil {
		t.Fatal("old entry dropped")
	}
}

// D) Streaming must use a real response write deadline rather than closing the
// source file, which cannot unblock a blocked ResponseWriter.
func TestStaticStreamSetsRealWriteDeadline(t *testing.T) {
	m, _, req, _ := staticFixture(t, siteFiles())
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	spy := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	r := httptest.NewRequest("GET", "http://workbench.test"+opened.URL, nil)
	m.Gateway(spy, r, func(hash string) bool { return hash == "login" })
	if spy.Code != 200 || spy.Body.Len() == 0 {
		t.Fatal(spy.Code, spy.Body.Len())
	}
	if !spy.deadlineSet {
		t.Fatal("stream did not set a response write deadline")
	}
	if d := time.Until(spy.deadline); d <= 0 || d > 60*time.Second {
		t.Fatalf("deadline %v", d)
	}
	// A writer that cannot set deadlines (httptest default) must still stream.
	plain := httptest.NewRecorder()
	m.Gateway(plain, httptest.NewRequest("GET", "http://workbench.test"+opened.URL, nil), func(hash string) bool { return hash == "login" })
	if plain.Code != 200 || plain.Body.String() != siteFiles()["index.html"] {
		t.Fatal(plain.Code, plain.Body.String())
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline    time.Time
	deadlineSet bool
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline, d.deadlineSet = t, true
	return nil
}
