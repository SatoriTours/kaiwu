// Package app owns the HTTP listener and its startup/shutdown lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/config"
	"github.com/SatoriTours/kaiwu/server/internal/httpapi"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/webapi"
	"github.com/SatoriTours/kaiwu/server/internal/webui"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Run validates local-only configuration before acquiring resources. Client
// connections do not own this context and cannot implicitly stop the server.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) (result error) {
	if ctx.Err() != nil {
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Workbench.Validate(); err != nil {
		return err
	}
	owned, cancelOwned := context.WithCancel(ctx)
	defer cancelOwned()
	ctx = owned
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if ctx.Err() != nil {
		return ln.Close()
	}
	defer func() {
		if result != nil {
			logger.Error("server stopped with error", "error", result)
		} else {
			logger.Info("server stopped")
		}
	}()
	handler := httpapi.NewHandler(logger)
	if cfg.Workbench.Token != "" {
		defer ln.Close()
		wb := cfg.Workbench
		store, err := workbench.Open(filepath.Join(wb.DataDir, "workbench.db"), workbench.Profile{Provider: wb.Provider, Model: wb.Model, Thinking: wb.Thinking})
		if err != nil {
			return fmt.Errorf("open workbench: %w", err)
		}
		defer func() { result = errors.Join(result, store.Close()) }()
		manager := agent.New(ctx, store, agent.Config{Program: wb.Program, DataDir: wb.DataDir, PiAgentDir: wb.PiAgentDir})
		defer func() { result = errors.Join(result, manager.Close()) }()
		previews := preview.New(ctx, store, preview.Config{DataDir: wb.DataDir, Reserve: manager.AcquirePreviewLease})
		manager.SetPreviewStopper(previews.StopTask)
		defer func() { result = errors.Join(result, previews.Close()) }()
		if err := manager.RecoverMerges(ctx); err != nil {
			return err
		}
		catalog := &pimodel.Catalog{Program: wb.Program, SourceDir: wb.PiAgentDir, DataDir: filepath.Join(wb.DataDir, "probes")}
		handler = webapi.New(webapi.Config{Previews: previews, Models: catalog, Git: manager, Context: ctx, Host: ln.Addr().String(), Token: wb.Token, Assets: webui.Files(), Health: handler, AgentAvailable: wb.Program != ""}, store, manager)
		defer func() {
			if closer, ok := handler.(interface{ Close() error }); ok {
				result = errors.Join(result, closer.Close())
			}
		}()
	}
	logger.Info("server started", "listen_addr", ln.Addr().String())
	result = serve(ctx, newServer(handler), ln, 5*time.Second, logger)
	cancelOwned()
	return result
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func serve(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	defer listener.Close()
	// net/http diagnostics must use the same structured log sink as lifecycle
	// events, including retryable Accept failures under resource pressure.
	srv.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelError)
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(listener) }()
	var result error
	select {
	case err := <-finished:
		// Unexpected accept failure must also reclaim active connections.
		result = errors.Join(serveError(err), srv.Close())
	case <-ctx.Done():
		// Shutdown must not inherit the already-canceled runtime context.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		err := srv.Shutdown(shutdownCtx)
		cancel()
		if err != nil {
			logger.Error("server forced close", "error", err)
			result = errors.Join(err, srv.Close())
		}
		result = errors.Join(result, serveError(<-finished))
	}
	return result
}

func serveError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
