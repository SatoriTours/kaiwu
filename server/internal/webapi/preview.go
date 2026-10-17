package webapi

import (
	"net/http"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
)

func (h *handler) taskPreview(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Previews == nil {
		fail(w, 503, "preview_unavailable", "任务预览服务不可用")
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "method_not_allowed", "预览操作需要 POST")
		return
	}
	var input preview.Request
	if !decode(w, r, &input) {
		return
	}
	var v preview.TaskPreview
	var err error
	switch strings.TrimPrefix(r.URL.Path, "/api/previews/") {
	case "status":
		v, err = h.cfg.Previews.Status(input)
	case "start":
		v, err = h.cfg.Previews.Start(input)
	case "stop":
		v, err = h.cfg.Previews.Stop(input)
	case "open":
		cookie, _ := r.Cookie("kaiwu_session")
		v, err = h.cfg.Previews.Open(input, credentialHash(cookie.Value))
	default:
		fail(w, 404, "not_found", "预览 API 不存在")
		return
	}
	if err != nil {
		h.failure(w, err)
		return
	}
	respond(w, 200, v)
}

func (h *handler) previewGateway(w http.ResponseWriter, r *http.Request) {
	// Only the capability path accepts the sandbox's opaque Origin. Host and all
	// normal API Origin/CSRF checks remain unchanged.
	check := r.Clone(r.Context())
	check.Header = r.Header.Clone()
	if check.Header.Get("Origin") == "null" {
		check.Header.Del("Origin")
		check.Header.Del("Sec-Fetch-Site")
	}
	if !h.allowed(check) {
		fail(w, 403, "forbidden_origin", "预览来源不受允许")
		return
	}
	if h.cfg.Previews == nil {
		fail(w, 404, "not_found", "预览服务不可用")
		return
	}
	h.cfg.Previews.Gateway(w, r, func(login string) bool { return h.store.ValidLogin(login, credentialHash(h.cfg.Token)) })
}
