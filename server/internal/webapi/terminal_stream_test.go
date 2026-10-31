//go:build linux

package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestTerminalStreamAcknowledgementResizeAndDisconnect(t *testing.T) {
	store, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, err := store.Command(workbench.Command{Type: "add_project", OperationID: "terminal-stream-project", Revision: store.Snapshot().Revision, Name: "terminal", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var h *handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	h = New(Config{Host: u.Host, Token: strings.Repeat("t", 32), DataDir: t.TempDir()}, store, nil).(*handler)
	defer h.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"token":"`+strings.Repeat("t", 32)+`"}`))
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("X-Kaiwu-Client", "web")
	req.Header.Set("Content-Type", "application/json")
	login, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer login.Body.Close()
	if login.StatusCode != 200 {
		t.Fatal(login.StatusCode)
	}
	term, err := h.terminals.Start(project.CreatedID, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/tools/terminal/socket?projectId=" + project.CreatedID + "&id=" + term.ID + "&v=2"
	headers := http.Header{"Origin": {srv.URL}, "Cookie": {login.Cookies()[0].String()}}
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(512 << 10)
	type frame struct {
		Data     []byte
		Sequence uint64
		Reset    bool
		Running  bool
	}
	var first frame
	if err = wsjson.Read(ctx, conn, &first); err != nil {
		t.Fatal(err)
	}
	if !first.Reset || !first.Running || first.Sequence != 1 {
		t.Fatal(first)
	}
	// Withhold ACK: data must remain server-side while input still works.
	if err = wsjson.Write(ctx, conn, map[string]any{"resize": map[string]int{"cols": 93, "rows": 29}, "input": "printf '\\377\\376'; printf 'SIZE:'; stty size\r"}); err != nil {
		t.Fatal(err)
	}
	next := make(chan frame, 1)
	failure := make(chan error, 1)
	go func() {
		var f frame
		if e := wsjson.Read(ctx, conn, &f); e != nil {
			failure <- e
		} else {
			next <- f
		}
	}()
	select {
	case <-next:
		t.Fatal("output sent before consumer ACK")
	case e := <-failure:
		t.Fatal(e)
	case <-time.After(100 * time.Millisecond):
	}
	if err = wsjson.Write(ctx, conn, map[string]any{"ack": first.Sequence}); err != nil {
		t.Fatal(err)
	}
	output := string(first.Data)
	for !strings.Contains(output, "SIZE:29 93\r\n") {
		select {
		case f := <-next:
			output += string(f.Data)
			if err = wsjson.Write(ctx, conn, map[string]any{"ack": f.Sequence}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "SIZE:29 93\r\n") {
				go func() {
					var f frame
					if e := wsjson.Read(ctx, conn, &f); e != nil {
						failure <- e
					} else {
						next <- f
					}
				}()
			}
		case e := <-failure:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatalf("no resized shell output: %q", output)
		}
	}
	if !strings.Contains(output, string([]byte{255, 254})) {
		t.Fatalf("wire corrupted non-UTF8 bytes: %q", output)
	}
	// Invalid dimensions close only the connection, never the shell.
	if err = wsjson.Write(ctx, conn, map[string]any{"resize": map[string]int{"cols": 0, "rows": 24}}); err != nil {
		t.Fatal(err)
	}
	for {
		_, _, err = conn.Read(ctx)
		if err != nil {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("invalid resize did not close connection")
	}
	snapshot, err := h.terminals.Read(term.ID)
	if err != nil || !snapshot.Running {
		t.Fatal("disconnect stopped shell", err)
	}
	// Wrong project scope cannot attach to this terminal, even with valid login.
	bad := strings.Replace(endpoint, "projectId="+project.CreatedID, "projectId=missing", 1)
	if c, _, e := websocket.Dial(ctx, bad, &websocket.DialOptions{HTTPHeader: headers}); e == nil {
		c.CloseNow()
		t.Fatal("cross-scope terminal access")
	}
	// Keep the legacy string transport usable for existing clients.
	legacy, _, err := websocket.Dial(ctx, strings.TrimSuffix(endpoint, "&v=2"), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.CloseNow()
	var old map[string]json.RawMessage
	if err = wsjson.Read(ctx, legacy, &old); err != nil {
		t.Fatal(err)
	}
	if _, ok := old["output"]; !ok {
		t.Fatal("legacy output missing")
	}
}
