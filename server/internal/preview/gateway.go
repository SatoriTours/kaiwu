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

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// staticSource expression builds the CSP source from the request Host after a
// strict charset check: an unvalidated Host would otherwise be able to inject CSP
// directives. Only a single origin plus the capability prefix is ever emitted.
func staticSource(r *http.Request, key string) (string, bool) {
	host := r.Host
	if host == "" || len(host) > 255 {
		return "", false
	}
	for _, c := range host {
		allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '-' || c == ':' || c == '[' || c == ']' || c == '_'
		if !allowed {
			return "", false
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	origin := scheme + "://" + host
	u, e := url.Parse(origin + "/")
	if e != nil || u.Scheme != scheme || u.Host != host || u.Path != "/" || u.User != nil || u.RawQuery != "" {
		return "", false
	}
	return origin + "/preview/" + key + "/", true
}

// staticCSP keeps task HTML in an opaque origin while letting its own relative
// CSS/JS/images/fonts load: every resource directive names the same validated
// capability prefix, no external host is allowed, connect-src cannot reach /api
// outside the prefix, and sandbox never grants same-origin.
func staticCSP(source string) string {
	return "sandbox allow-scripts; default-src 'none'; " +
		"script-src 'unsafe-inline' 'unsafe-eval' " + source + "; " +
		"style-src 'unsafe-inline' " + source + "; " +
		"img-src data: blob: " + source + "; " +
		"font-src data: " + source + "; " +
		"media-src data: " + source + "; " +
		"connect-src " + source + "; " +
		"frame-ancestors 'self'; base-uri 'none'; form-action 'none'"
}

// serveStatic answers one static asset for a capability. It revalidates login,
// task/worktree/round/manifest on every request, allows GET/HEAD only and never
// lists a directory or proxies another host.
func (m *Manager) serveStatic(w http.ResponseWriter, r *http.Request, key string, token *capability, tail string, validLogin func(string) bool) {
	source, ok := staticSource(r, key)
	if !ok {
		gatewayError(w, 400, "预览来源主机无效")
		return
	}
	en := token.entry
	view, e := m.stateStatic(en.req, en)
	if e != nil || view.State != "running" {
		gatewayError(w, 409, "预览目标已变化或停止，请回到任务刷新")
		return
	}
	if !validLogin(token.login) {
		gatewayError(w, 401, "预览链接无效、登录已退出或已过期")
		return
	}
	// The URL path after the capability is relative to the manifest root, so the
	// entry and its relative CSS/JS/images resolve inside the same sandbox. The
	// reserved download segment is never a static asset.
	if strings.HasPrefix(tail, downloadPrefix) {
		m.serveDownload(w, r, key, token, strings.TrimPrefix(tail, downloadPrefix), source)
		return
	}
	root, rel := view.Root, strings.TrimPrefix(tail, "/")
	if root == "" || rel == "" {
		gatewayError(w, 404, "预览资源不可用")
		return
	}
	m.mu.Lock()
	still := m.tokens[key] == token && m.entries[en.req.TaskID] == en && !m.closed && !time.Now().After(token.expires)
	m.mu.Unlock()
	if !still {
		gatewayError(w, 401, "预览链接无效或已过期")
		return
	}
	f, info, mime, e := openStaticFile(en.tree, root, rel)
	if e != nil {
		gatewayError(w, 404, "预览资源不可用")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", staticCSP(source))
	if r.Method == "HEAD" {
		w.WriteHeader(200)
		return
	}
	// A real response write deadline, not a goroutine closing the source file:
	// closing the reader cannot unblock a stalled ResponseWriter. Servers that
	// cannot set one (httptest) still stream under the size bound below.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.WriteHeader(200)
	_, _ = io.CopyN(w, f, info.Size())
}

// downloadPrefix is the only path segment that can reach a declared file. It is
// reserved, so a static asset may never shadow or be shadowed by it.
const downloadPrefix = "~download/"

// serveDownload streams one explicitly declared file as an attachment. The
// browser supplies only the opaque artifact ID; the path is re-resolved from the
// current manifest, opened with the same fd-relative NOFOLLOW walk as static
// assets, and never inherited as a document in the workbench origin.
func (m *Manager) serveDownload(w http.ResponseWriter, r *http.Request, key string, token *capability, id, source string) {
	en := token.entry
	view, snap, confirmed, e := m.stateStaticSnapshot(en.req, en)
	if e != nil || view.State != "running" {
		gatewayError(w, 409, "预览目标已变化或停止，请回到任务刷新")
		return
	}
	if !validLoginShape(id) {
		gatewayError(w, 404, "文件不可用")
		return
	}
	if !confirmed {
		// The state check above could not be tied to one validated manifest
		// snapshot, so no declaration can be proven to belong to the approved
		// capability.
		gatewayError(w, 409, "预览目标已变化或停止，请回到任务刷新")
		return
	}
	// Resolve strictly inside that same snapshot: a manifest written after the
	// check must never add a file to an already-issued capability.
	artifact, e := resolveArtifactIn(en.tree, snap, id)
	if e != nil || artifact.Size > maxStaticFileSize {
		gatewayError(w, 404, "文件不可用")
		return
	}
	m.mu.Lock()
	still := m.tokens[key] == token && m.entries[en.req.TaskID] == en && !m.closed && !time.Now().After(token.expires)
	m.mu.Unlock()
	if !still {
		gatewayError(w, 401, "预览链接无效或已过期")
		return
	}
	f, info, mime, e := openArtifactFile(en.tree, artifact.Path)
	if e != nil || !info.Mode().IsRegular() || info.Size() != artifact.Size || mime != artifact.Type {
		if f != nil {
			f.Close()
		}
		gatewayError(w, 404, "文件不可用")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", artifact.Type)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Content-Disposition", downloadDisposition(artifact.Name))
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", staticCSP(source))
	if r.Method == "HEAD" {
		w.WriteHeader(200)
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.WriteHeader(200)
	_, _ = io.CopyN(w, f, info.Size())
}

// validLoginShape accepts only a 64-char lowercase hex artifact ID, so no path
// text, traversal or separator ever reaches resolution.
func validLoginShape(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// stateStatic answers whether the static entry is still the current one for its
// round and fingerprint without taking a process/port identity into account.
func (m *Manager) stateStatic(req Request, en *entry) (TaskPreview, error) {
	v, _, _, e := m.stateStaticSnapshot(req, en)
	return v, e
}

// stateStaticSnapshot also returns the validated manifest snapshot the accepted
// state was derived from. A download must resolve its declared path from this
// snapshot rather than reading the manifest again, otherwise a write that lands
// between the state check and the read would let an already-issued capability
// address a declaration it was never approved for.
func (m *Manager) stateStaticSnapshot(req Request, en *entry) (TaskPreview, manifest, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return TaskPreview{}, manifest{}, false, workbench.Conflict("预览服务已关闭")
	}
	v, snap, ok, e := m.stateLockedSnapshot(req)
	if e != nil {
		return v, manifest{}, false, e
	}
	if m.entries[req.TaskID] != en || !worktreeKind(v.Kind) || en.view.ManifestID != v.ManifestID {
		return v, manifest{}, false, workbench.Conflict("静态预览目标已变化")
	}
	return v, snap, ok, nil
}

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
	if !ok || len(key) == 0 {
		gatewayError(w, 401, "预览链接无效或已过期")
		return
	}
	// url.Path is already decoded, so a client-sent %2f/%5c would otherwise be
	// indistinguishable from a real separator. Check the still-encoded form
	// before any decoding or segment split, and only for the capability path.
	if encodedPathSeparator(r) {
		gatewayError(w, 400, "预览路径无效")
		return
	}
	if len(key) != 64 {
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
	if worktreeKind(en.kind) {
		// Worktree serving never touches a process or port; revalidation and the
		// open itself happen outside the manager lock.
		m.mu.Unlock()
		m.serveStatic(w, r, key, token, tail, validLogin)
		return
	}
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

// encodedPathSeparator reports whether the request path still carries a
// percent-encoded path separator ("%2f" or "%5c", any case) after the single
// decoding net/http already performed. Such a request would address a path the
// browser never spelled, so it is refused before the capability is split.
// Ordinary percent-encoded names ("%20", "%25", non-ASCII UTF-8) stay allowed.
// The route prefix may itself be percent-encoded ("/pre%76iew/"), which is why
// the raw literal /preview/ prefix is not what authorizes the check.
func encodedPathSeparator(r *http.Request) bool {
	raw := r.URL.RawPath
	if raw == "" {
		raw = r.RequestURI
		if i := strings.IndexAny(raw, "?#"); i >= 0 {
			raw = raw[:i]
		}
	}
	// Routing above already matched the decoded URL.Path against /preview/, so
	// the request is a preview request whatever the raw spelling of the prefix
	// ("/pre%76iew/" decodes to "/preview/"). Matching the raw literal prefix
	// here would let such a spelling bypass the guard, so only the encoded
	// separator itself decides.
	if raw == "" {
		return false
	}
	for i := 0; i+2 < len(raw); i++ {
		if raw[i] != '%' {
			continue
		}
		switch raw[i+1 : i+3] {
		case "2f", "2F", "5c", "5C":
			return true
		}
	}
	return false
}
