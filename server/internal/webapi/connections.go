package webapi

import (
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"net/http"
	"strings"
)

func (h *handler) connectionError(w http.ResponseWriter, err error) {
	status := 400
	code := "connection_error"
	if errors.Is(err, pimodel.ErrConnectionConflict) {
		status = 409
		code = "conflict"
	}
	fail(w, status, code, err.Error())
}
func (h *handler) piConnections(w http.ResponseWriter, r *http.Request) {
	c := h.cfg.Connections
	if c == nil {
		fail(w, 503, "connections_unavailable", "连接配置服务不可用")
		return
	}
	cookie, _ := r.Cookie("kaiwu_session")
	owner := credentialHash(cookie.Value)
	path := strings.TrimPrefix(r.URL.Path, "/api/pi/")
	if path == "connections" && r.Method == "GET" {
		respond(w, 200, map[string]any{"connections": c.List(), "oauthProviders": []map[string]string{{"value": "openai", "label": "OpenAI · ChatGPT 订阅"}, {"value": "anthropic", "label": "Anthropic · Claude 订阅"}}})
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "method_not_allowed", "不支持的请求方法")
		return
	}
	switch path {
	case "connections/save", "connections/models":
		var in pimodel.ConnectionInput
		if !decode(w, r, &in) {
			return
		}
		if path == "connections/models" {
			models, err := c.Discover(r.Context(), in)
			if err != nil {
				h.connectionError(w, err)
				return
			}
			respond(w, 200, map[string]any{"models": models})
			return
		}
		record, err := c.Save(in)
		if err != nil {
			h.connectionError(w, err)
			return
		}
		h.invalidateModels()
		respond(w, 200, map[string]any{"connection": record})
	case "connections/remove":
		var in struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := c.Remove(in.ID, in.Revision); err != nil {
			h.connectionError(w, err)
			return
		}
		h.invalidateModels()
		respond(w, 200, map[string]bool{"ok": true})
	case "oauth/start":
		var in pimodel.OAuthInput
		if !decode(w, r, &in) {
			return
		}
		result, err := c.StartOAuth(owner, in)
		if err != nil {
			h.connectionError(w, err)
			return
		}
		respond(w, 200, result)
	case "oauth/finish":
		var in struct {
			ID       string `json:"id"`
			Callback string `json:"callback"`
		}
		if !decode(w, r, &in) {
			return
		}
		record, err := c.FinishOAuth(r.Context(), owner, in.ID, in.Callback)
		if err != nil {
			h.connectionError(w, err)
			return
		}
		h.invalidateModels()
		respond(w, 200, map[string]any{"connection": record})
	case "oauth/cancel":
		var in struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &in) {
			return
		}
		c.CancelOAuth(owner, in.ID)
		respond(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 404, "not_found", "连接配置 API 不存在")
	}
}
func (h *handler) invalidateModels() {
	if catalog, ok := h.cfg.Models.(interface{ InvalidateCache() }); ok {
		catalog.InvalidateCache()
	}
}
