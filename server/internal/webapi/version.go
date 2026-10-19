package webapi

import (
	"net/http"

	"github.com/SatoriTours/kaiwu/server/internal/buildinfo"
)

// version reports the running binary's own revision and the last recorded
// deployment attempt. The response is never cached, the running revision comes
// only from the binary, and unknown values stay unknown instead of claiming a
// successful release.
func (h *handler) version(w http.ResponseWriter, _ *http.Request) {
	var status buildinfo.DeploymentStatus
	if h.cfg.Status != nil {
		status = h.cfg.Status()
	}
	respond(w, 200, buildinfo.Report(h.cfg.Version, status))
}
