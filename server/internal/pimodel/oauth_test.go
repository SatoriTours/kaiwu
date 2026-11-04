package pimodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type oauthTransport func(*http.Request) (*http.Response, error)

func (f oauthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOAuthPKCEOwnerStateAndSingleUse(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	start, err := c.StartOAuth("owner-A", OAuthInput{Provider: "openai", Name: "我的 ChatGPT"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.URL)
	state := u.Query().Get("state")
	if u.Host != "auth.openai.com" || u.Query().Get("code_challenge") == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("unsafe auth URL")
	}
	calls := 0
	c.oauthClient = &http.Client{Transport: oauthTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://auth.openai.com/api/accounts/oauth/token" {
			t.Fatal("wrong token target")
		}
		_ = r.ParseForm()
		if r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "synthetic-code" {
			t.Fatal("missing PKCE")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","id_token":"id","scope":"openid chatgpt.tokens.use.direct","expires_in":3600}`)), Header: http.Header{}}, nil
	})}
	callback := "http://127.0.0.1:1455/auth/callback?code=synthetic-code&state=" + state + "&client_id=issued"
	if _, err = c.FinishOAuth(context.Background(), "owner-B", start.ID, callback); err == nil {
		t.Fatal("owner bypass")
	}
	if _, err = c.FinishOAuth(context.Background(), "owner-A", start.ID, strings.Replace(callback, state, "wrong", 1)); err == nil {
		t.Fatal("state bypass")
	}
	if calls != 0 {
		t.Fatal("token sent before verification")
	}
	conn, err := c.FinishOAuth(context.Background(), "owner-A", start.ID, callback)
	if err != nil || conn.Provider != "openai" || conn.Kind != "oauth" {
		t.Fatal("finish", err)
	}
	if _, err = c.FinishOAuth(context.Background(), "owner-A", start.ID, callback); err == nil || calls != 1 {
		t.Fatal("replayed")
	}
	b, _ := json.Marshal(c.List())
	if strings.Contains(string(b), "synthetic-") {
		t.Fatal("tokens exposed")
	}
	if _, err = c.StartOAuth("owner-A", OAuthInput{Provider: "openai", Name: "duplicate"}); err == nil {
		t.Fatal("duplicate provider")
	}
}
func TestOAuthExpirationCancellationAndAnthropic(t *testing.T) {
	c, _ := OpenConnections(t.TempDir())
	start, err := c.StartOAuth("a", OAuthInput{Provider: "anthropic", Name: "Claude"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.URL)
	if u.Query().Get("redirect_uri") != "https://platform.claude.com/oauth/code/callback" {
		t.Fatal("not headless flow")
	}
	p := c.pending[start.ID]
	p.Expires = time.Now().Add(-time.Second)
	c.pending[start.ID] = p
	if _, err = c.FinishOAuth(context.Background(), "a", start.ID, "code#"+u.Query().Get("state")); err == nil {
		t.Fatal("expired accepted")
	}
	start, _ = c.StartOAuth("a", OAuthInput{Provider: "anthropic", Name: "Claude"})
	c.CancelOAuth("b", start.ID)
	if _, ok := c.pending[start.ID]; !ok {
		t.Fatal("cross owner cancel")
	}
	c.CancelOAuth("a", start.ID)
	if _, ok := c.pending[start.ID]; ok {
		t.Fatal("not cancelled")
	}
	if _, err = c.StartOAuth("a", OAuthInput{Provider: "unknown", Name: "bad"}); err == nil {
		t.Fatal("unsupported OAuth")
	}
}
