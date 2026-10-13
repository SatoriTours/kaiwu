package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

type observedWriter struct {
	http.ResponseWriter
	status int
}

func (w *observedWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.ResponseWriter.WriteHeader(status)
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.status = status
	}
}

func (w *observedWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *observedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// FlushError keeps response commitment observable even when a handler uses
// ResponseController rather than Write. Otherwise a later panic could append
// an invalid error body to an already-flushed response.
func (w *observedWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func observeAndRecover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := time.Now()
		observed := &observedWriter{ResponseWriter: w}
		defer func() {
			caught := recover()
			aborted := caught != nil
			if caught != nil && caught != http.ErrAbortHandler {
				// Panic values can contain secrets; never log them or a stack.
				logger.Error("handler panic")
				if observed.status == 0 {
					writeError(observed, req, http.StatusInternalServerError, "internal_error", "internal server error")
					aborted = false
				}
			}
			status := observed.status
			if status == 0 && !aborted {
				status = http.StatusOK
			}
			logger.Info("http request", "method", req.Method, "path", req.URL.Path,
				"status", status, "duration", time.Since(started), "aborted", aborted)
			if aborted {
				panic(http.ErrAbortHandler)
			}
		}()
		next.ServeHTTP(observed, req)
	})
}
