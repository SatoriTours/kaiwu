package preview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func upstreamClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 64 << 10}}
}

func gatewayError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}

// Gateway uses only the capability. Browser cookies are neither required nor
// forwarded. validLogin rechecks the original login hash on every request.
func (m *Manager) Gateway(w http.ResponseWriter, r *http.Request, validLogin func(string) bool) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		gatewayError(w, 405, "预览只支持 GET/HEAD")
		return
	}
	key, tail, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/preview/"), "/")
	if !ok || len(key) != 64 {
		gatewayError(w, 401, "预览链接无效或已过期")
		return
	}
	m.mu.Lock()
	token := m.tokens[key]
	if m.closed || token == nil || time.Now().After(token.expires) || validLogin == nil || !validLogin(token.login) {
		m.mu.Unlock()
		gatewayError(w, 401, "预览链接无效、登录已退出或已过期")
		return
	}
	en := token.entry
	// Revalidate task/worktree/round/manifest, including external mutations.
	view, e := m.stateLocked(en.req)
	if e != nil || view.State != "running" || m.entries[en.req.TaskID] != en || ownedPort(en.port, en.cmd.Process.Pid) != nil {
		m.mu.Unlock()
		gatewayError(w, 409, "预览目标已变化或停止，请回到任务刷新")
		return
	}
	port := en.port
	m.mu.Unlock()
	if strings.ContainsAny(tail, "\\\x00\r\n") {
		gatewayError(w, 400, "预览路径无效")
		return
	}
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port), Path: "/" + tail, RawQuery: r.URL.RawQuery}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	request, e := http.NewRequestWithContext(ctx, r.Method, target.String(), nil)
	if e != nil {
		gatewayError(w, 400, "预览路径无效")
		return
	}
	// Do not clone the inbound headers: no Cookie, Authorization, Origin,
	// Referer, forwarding headers, or workbench token reaches the task process.
	request.Header.Set("Accept", r.Header.Get("Accept"))
	response, e := upstreamClient().Do(request)
	if e != nil {
		gatewayError(w, 502, "预览 HTTP 请求失败")
		return
	}
	defer response.Body.Close()
	m.mu.Lock()
	still := m.tokens[key] == token && en.lease != nil && ownedPort(port, en.cmd.Process.Pid) == nil
	m.mu.Unlock()
	if !still || !validLogin(token.login) {
		gatewayError(w, 409, "预览已停止或归属变化")
		return
	}
	if location := response.Header.Get("Location"); location != "" {
		u, e := url.Parse(location)
		if e != nil || strings.ContainsAny(location, "\\\r\n\x00") {
			gatewayError(w, 502, "预览重定向无效")
			return
		}
		resolved := target.ResolveReference(u)
		if resolved.Scheme != target.Scheme || resolved.Host != target.Host || resolved.User != nil || !safePath(resolved.Path) {
			gatewayError(w, 502, "预览不允许跳转到外部地址")
			return
		}
		w.Header().Set("Location", "/preview/"+key+resolved.RequestURI())
	}
	// Only expose content, never upstream Set-Cookie, CORS, Refresh, CSP or Link.
	if content := response.Header.Get("Content-Type"); content != "" {
		w.Header().Set("Content-Type", content)
	}
	w.Header().Del("X-Frame-Options")
	source := "http://" + r.Host + "/preview/" + key + "/"
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' "+source+"; style-src 'unsafe-inline' "+source+"; img-src data: blob: "+source+"; font-src "+source+"; media-src "+source+"; connect-src "+source+"; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	if r.Header.Get("Origin") == "null" {
		w.Header().Set("Access-Control-Allow-Origin", "null")
		w.Header().Set("Vary", "Origin")
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != "HEAD" {
		_, _ = io.Copy(w, io.LimitReader(response.Body, 16<<20))
	}
}
