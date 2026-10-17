package webapi

import (
	"context"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSocketCommandsReplayAndDelta(t *testing.T) {
	store, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	handler = New(Config{Host: u.Host, Token: strings.Repeat("x", 32)}, store, nil)
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"token":"`+strings.Repeat("x", 32)+`"}`))
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kaiwu-Client", "web")
	res, e := srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, strings.Replace(srv.URL, "http:", "ws:", 1)+"/api/socket", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}, "Cookie": {res.Cookies()[0].String()}}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	c.SetReadLimit(2 << 20)
	send := func(id, method string, body any) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"id": id, "method": method, "body": body})
		if e := c.Write(ctx, websocket.MessageText, b); e != nil {
			t.Fatal(e)
		}
	}
	var chunks strings.Builder
	chunked := false
	readReply := func(id string) map[string]any {
		t.Helper()
		for {
			_, b, e := c.Read(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var v map[string]any
			json.Unmarshal(b, &v)
			switch v["type"] {
			case "snapshot_start":
				chunks.Reset()
			case "snapshot_chunk":
				chunks.WriteString(v["data"].(string))
			case "snapshot_end":
				chunked = true
				if !json.Valid([]byte(chunks.String())) {
					t.Fatal("invalid reconstructed snapshot")
				}
			}
			if v["id"] == id {
				return v
			}
		}
	}
	send("sync", "sync", map[string]any{"after": 0})
	if r := readReply("sync"); r["status"] != float64(200) {
		t.Fatal(r)
	}
	body := map[string]any{"operationId": "socket-project", "type": "add_project", "revision": store.Snapshot().Revision, "name": "socket", "path": t.TempDir()}
	send("one", "commands", body)
	r := readReply("one")
	if r["status"] != float64(200) {
		t.Fatal(r)
	}
	send("two", "commands", body)
	if r := readReply("two"); r["status"] != float64(200) {
		t.Fatal(r)
	}
	if len(store.Snapshot().Projects) != 1 {
		t.Fatal("duplicate side effect")
	}
	send("models-refresh", "models?refresh=1", map[string]any{})
	if r := readReply("models-refresh"); r["status"] != float64(503) {
		t.Fatal("model refresh not routed", r)
	}
	send("bad", "../../healthz", nil)
	if r := readReply("bad"); r["status"] != float64(400) {
		t.Fatal(r)
	}

	if e := store.Update(func(st *workbench.State) error {
		for id, conv := range st.Conversations {
			conv.Messages = append(conv.Messages, workbench.Message{ID: "large", Role: "assistant", Text: strings.Repeat("测", 100000), Delivery: "complete"})
			st.Conversations[id] = conv
			break
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	send("large-sync", "sync", map[string]any{"after": 0})
	if r := readReply("large-sync"); r["status"] != float64(200) || !chunked {
		t.Fatal("snapshot chunks were not delivered", r)
	}
	if e := handler.(interface{ Close() error }).Close(); e != nil {
		t.Fatal(e)
	}
	if _, _, e := c.Read(ctx); e == nil {
		t.Fatal("owner close left socket running")
	}

}
