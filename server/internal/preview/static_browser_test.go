//go:build staticbrowser

package preview

import (
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestStaticGatewayServesRealBrowser is a manual harness (build tag
// staticbrowser, excluded from the normal suite) that exposes the real static
// gateway on a loopback port for a Playwright-driven check.
func TestStaticGatewayServesRealBrowser(t *testing.T) {
	m, _, req, _ := staticFixture(t, map[string]string{
		"index.html":     `<!doctype html><html><head><link rel="stylesheet" href="assets/app.css"></head><body><div id="styled">x</div><img id="img" src="img/pic.png"><script src="assets/app.js"></script></body></html>`,
		"assets/app.css": "#styled{color:rgb(1,2,3);width:123px}",
		"assets/app.js":  `document.getElementById('styled').dataset.ran='yes';fetch('/api/tasks').then(r=>{document.title='fetch-status-'+r.status}).catch(()=>{document.title='fetch-blocked'})`,
		"img/pic.png":    "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82",
	})
	opened, e := m.Open(req, "login")
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.Gateway(w, r, func(h string) bool { return h == "login" })
	})}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go srv.Serve(ln)
	defer srv.Close()
	url := "http://" + ln.Addr().String() + opened.URL
	if e := os.WriteFile(os.Getenv("KAIWU_BROWSER_URL"), []byte(url), 0o600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(os.Getenv("KAIWU_BROWSER_DONE")); e == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("browser harness timed out")
}
