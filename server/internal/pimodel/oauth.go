package pimodel

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OAuthInput struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	ID       string `json:"id,omitempty"`
	Revision int    `json:"revision,omitempty"`
}
type OAuthStart struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Provider string `json:"provider"`
	Expires  int64  `json:"expires"`
}
type oauthPending struct {
	Owner, Verifier, State string
	Input                  OAuthInput
	Expires                time.Time
}

const anthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
const anthropicRedirect = "https://platform.claude.com/oauth/code/callback"
const openaiRedirect = "http://127.0.0.1:1455/auth/callback"

func (c *Connections) StartOAuth(owner string, in OAuthInput) (OAuthStart, error) {
	if owner == "" || !safeString(strings.TrimSpace(in.Name), 80) {
		return OAuthStart{}, errors.New("请输入连接名称")
	}
	if in.Provider != "openai" && in.Provider != "anthropic" {
		return OAuthStart{}, errors.New("此提供商尚不支持网页账号授权，请使用 API Key 或服务器 pi 登录")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, p := range c.pending {
		if time.Now().After(p.Expires) {
			delete(c.pending, key)
		}
	}
	if len(c.pending) >= 16 {
		return OAuthStart{}, errors.New("待授权连接过多，请取消旧授权或稍后重试")
	}
	if in.ID != "" {
		r, ok := c.find(in.ID)
		if !ok || r.Revision != in.Revision || r.Kind != "oauth" || r.Provider != in.Provider {
			return OAuthStart{}, ErrConnectionConflict
		}
	}
	for _, r := range c.data.Records {
		if r.Provider == in.Provider && r.ID != in.ID {
			return OAuthStart{}, errors.New("该提供商已有账号，请编辑并重新授权")
		}
	}
	if in.ID == "" && len(c.data.Records) >= 32 {
		return OAuthStart{}, errors.New("最多保存 32 个连接")
	}
	verifier := randomID() + randomID()
	state := randomID() + randomID()
	if in.Provider == "anthropic" {
		state = verifier
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{"response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}, "state": {state}}
	target := "https://claude.ai/oauth/authorize"
	if in.Provider == "anthropic" {
		q.Set("client_id", anthropicClientID)
		q.Set("redirect_uri", anthropicRedirect)
		q.Set("code", "true")
		q.Set("scope", "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload")
	} else {
		target = "https://auth.openai.com/api/accounts/authorize"
		id := c.data.HostID
		q.Set("client_id", "dynamic_agent_client")
		q.Set("agent_name_hint", "Pi")
		q.Set("ext_agent_host_id", "urn:uuid:"+id[:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:])
		q.Set("redirect_uri", openaiRedirect)
		q.Set("resource", "https://api.openai.com/v1")
		q.Set("scope", "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct")
		q.Set("nonce", randomID())
	}
	id := randomID()
	expires := time.Now().Add(10 * time.Minute)
	c.pending[id] = oauthPending{Owner: owner, Verifier: verifier, State: state, Input: in, Expires: expires}
	return OAuthStart{ID: id, URL: target + "?" + q.Encode(), Provider: in.Provider, Expires: expires.UnixMilli()}, nil
}
func (c *Connections) CancelOAuth(owner, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.pending[id]; ok && p.Owner == owner {
		delete(c.pending, id)
	}
}
func (c *Connections) FinishOAuth(ctx context.Context, owner, id, input string) (Connection, error) {
	c.mu.Lock()
	p, ok := c.pending[id]
	if !ok || p.Owner != owner || time.Now().After(p.Expires) {
		c.mu.Unlock()
		return Connection{}, errors.New("授权已失效，请重新发起")
	}
	if len(input) > 8192 {
		c.mu.Unlock()
		return Connection{}, errors.New("授权回调内容过长")
	}
	var code, state, clientID string
	u, err := url.Parse(strings.TrimSpace(input))
	if err == nil && u.Scheme != "" {
		expected := anthropicRedirect
		if p.Input.Provider == "openai" {
			expected = openaiRedirect
		}
		e, _ := url.Parse(expected)
		if u.Scheme != e.Scheme || u.Host != e.Host || u.Path != e.Path || u.User != nil || u.Fragment != "" {
			c.mu.Unlock()
			return Connection{}, errors.New("请粘贴本次授权页面的完整回调 URL")
		}
		code = u.Query().Get("code")
		state = u.Query().Get("state")
		clientID = u.Query().Get("client_id")
	} else if p.Input.Provider == "anthropic" {
		parts := strings.SplitN(strings.TrimSpace(input), "#", 2)
		if len(parts) == 2 {
			code, state = parts[0], parts[1]
		}
	}
	if code == "" || state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(p.State)) != 1 || (p.Input.Provider == "openai" && !safeString(clientID, 300)) {
		c.mu.Unlock()
		return Connection{}, errors.New("授权码或 state 不匹配，请使用本次授权的回调内容")
	}
	delete(c.pending, id)
	c.mu.Unlock()
	// Consume once before networking. A failed/unknown exchange requires a fresh grant.
	target := "https://platform.claude.com/v1/oauth/token"
	var body string
	contentType := "application/json"
	if p.Input.Provider == "anthropic" {
		b, _ := json.Marshal(map[string]string{"grant_type": "authorization_code", "client_id": anthropicClientID, "code": code, "state": state, "redirect_uri": anthropicRedirect, "code_verifier": p.Verifier})
		body = string(b)
	} else {
		target = "https://auth.openai.com/api/accounts/oauth/token"
		contentType = "application/x-www-form-urlencoded"
		body = url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "code_verifier": {p.Verifier}, "redirect_uri": {openaiRedirect}, "resource": {"https://api.openai.com/v1"}}.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	client := c.oauthClient
	if client == nil {
		client = credentialClient()
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return Connection{}, errors.New("授权交换未完成，请重新授权；未自动重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Connection{}, errors.New("提供商拒绝授权交换，请重新授权")
	}
	b, err := readLimited(resp.Body, 64<<10)
	if err != nil {
		return Connection{}, errors.New("授权响应不完整，请重新授权")
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
		Scope   string `json:"scope"`
		ID      string `json:"id_token"`
	}
	if json.Unmarshal(b, &token) != nil || token.Access == "" || token.Refresh == "" || token.Expires <= 0 || token.Expires > 31536000 {
		return Connection{}, errors.New("提供商授权响应缺少必需信息")
	}
	margin := int64(300000)
	if p.Input.Provider == "openai" {
		margin = 180000
		if token.ID == "" || !containsScope(token.Scope, "chatgpt.tokens.use.direct") {
			return Connection{}, errors.New("授权不包含 ChatGPT 直接调用权限，请重新授权")
		}
	}
	auth := map[string]any{"type": "oauth", "access": token.Access, "refresh": token.Refresh, "expires": time.Now().UnixMilli() + token.Expires*1000 - margin}
	if p.Input.Provider == "openai" {
		auth["clientId"] = clientID
		auth["scopes"] = strings.Fields(token.Scope)
	}
	raw, _ := json.Marshal(auth)
	c.mu.Lock()
	defer c.mu.Unlock()
	record, exists := c.find(p.Input.ID)
	if p.Input.ID != "" && (!exists || record.Revision != p.Input.Revision) {
		return Connection{}, ErrConnectionConflict
	}
	for _, r := range c.data.Records {
		if r.Provider == p.Input.Provider && r.ID != p.Input.ID {
			return Connection{}, ErrConnectionConflict
		}
	}
	if !exists {
		if len(c.data.Records) >= 32 {
			return Connection{}, errors.New("连接数量已达上限")
		}
		record.Connection = Connection{ID: randomID(), Kind: "oauth", Provider: p.Input.Provider, Models: []string{}}
	}
	record.Name = strings.TrimSpace(p.Input.Name)
	record.Revision++
	record.Auth = raw
	record.HasKey = true
	records := append([]connectionRecord{}, c.data.Records...)
	if exists {
		for i := range records {
			if records[i].ID == record.ID {
				records[i] = record
			}
		}
	} else {
		records = append(records, record)
	}
	if err = c.commit(records); err != nil {
		return Connection{}, err
	}
	return cloneConnection(record.Connection), nil
}
func containsScope(value, want string) bool {
	for _, s := range strings.Fields(value) {
		if s == want {
			return true
		}
	}
	return false
}
