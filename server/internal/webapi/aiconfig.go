package webapi

import (
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/aiconfig"
	"io"
	"net/http"
)

func (h *handler) aiConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		view, err := h.store.AIConfig(r.URL.Query().Get("projectId"))
		if err != nil {
			h.aiConfigFailure(w, err)
			return
		}
		respond(w, 200, view)
		return
	}
	var body struct {
		ProjectID string          `json:"projectId"`
		Version   string          `json:"version"`
		Config    aiconfig.Config `json:"config"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_config", "配置字段或格式无效，不接受凭据正文")
		return
	}
	view, err := h.store.SaveAIConfig(body.ProjectID, body.Version, body.Config)
	if err != nil {
		h.aiConfigFailure(w, err)
		return
	}
	respond(w, 200, view)
}
func (h *handler) aiConfigFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, aiconfig.ErrConflict) {
		fail(w, 409, "config_conflict", "配置版本已变化或正在写入，请重新读取；未覆盖文件")
		return
	}
	fail(w, 400, "invalid_config", "配置不可用或超出继承授权范围，请检查版本、模型、技能及凭据引用")
}
