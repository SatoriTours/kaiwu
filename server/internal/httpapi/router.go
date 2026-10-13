// Package httpapi exposes the product's HTTP API, independent of environment
// configuration and process ownership.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

// NewHandler creates the local foundation API. Health is not Agent readiness.
func NewHandler(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", health)
	r.Head("/healthz", health)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeError(w, req, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		// chi rejects unregistered methods before checking the path. Match the
		// same routing path without mutating the request's route context.
		path := req.URL.RawPath
		if path == "" {
			path = req.URL.Path
		}
		if !r.Match(chi.NewRouteContext(), http.MethodGet, path) {
			writeError(w, req, http.StatusNotFound, "not_found", "route not found")
			return
		}
		// This foundation exposes only GET/HEAD routes. Revisit Allow when
		// registering business endpoints with other supported methods.
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, req, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})
	return observeAndRecover(logger, r)
}

func health(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, req, http.StatusOK, healthResponse{Status: "ok", Service: "kaiwu-server"})
}

func writeError(w http.ResponseWriter, req *http.Request, status int, code, message string) {
	writeJSON(w, req, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, req *http.Request, status int, value any) {
	// Encode before committing headers: HEAD describes the same representation
	// as GET, and encoding failure must not leave an empty success response.
	body, err := json.Marshal(value)
	if err != nil {
		panic("cannot encode JSON response")
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if req.Method != http.MethodHead {
		// Connection write failures cannot be repaired by writing another body.
		_, _ = w.Write(body)
	}
}
