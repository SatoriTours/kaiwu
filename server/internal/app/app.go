// Package app owns the HTTP listener and its startup/shutdown lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/buildinfo"
	"github.com/SatoriTours/kaiwu/server/internal/config"
	"github.com/SatoriTours/kaiwu/server/internal/delivery"
	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/httpapi"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/webapi"
	"github.com/SatoriTours/kaiwu/server/internal/webui"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// buildDelivery assembles the automated-delivery worker from the service
// credentials. Without API and Git credentials the feature stays disabled and
// the basic server still starts; there is no fake-endpoint switch in
// production.
func buildDelivery(ctx context.Context, dataDir string, store *workbench.Store, agentPort delivery.AgentPort) (*delivery.Manager, error) {
	gh := config.LoadGitHub(os.LookupEnv)
	if gh.APIToken == "" || (gh.GitToken == "" && gh.SSHKey == "") {
		return nil, nil
	}
	resolver, err := githubauth.New(githubauth.Config{Lookup: os.LookupEnv})
	if err != nil {
		return nil, fmt.Errorf("delivery credentials: %w", err)
	}
	actions, err := githubactions.NewClient(githubactions.ClientConfig{Token: resolver.Token})
	if err != nil {
		return nil, fmt.Errorf("delivery actions: %w", err)
	}
	remote := gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver})
	return delivery.New(ctx, store, delivery.Config{
		Agent:   agentPort,
		Actions: actions,
		Remote:  remote,
		Clock:   delivery.SystemClock{},
		DataDir: dataDir,
	})
}

// manualRemoteRunner builds the read-only remote runner used by manual push
// review. It shares the existing credential resolver but never builds delivery
// workers, policies or API clients.
func manualRemoteRunner() (*gitops.RemoteRunner, error) {
	resolver, err := githubauth.New(githubauth.Config{Lookup: os.LookupEnv})
	if err != nil {
		return nil, fmt.Errorf("manual review credentials: %w", err)
	}
	return gitops.NewRemoteRunner(gitops.RemoteConfig{Resolver: resolver}), nil
}

// deliveryCommander keeps a disabled delivery out of the Web API as a real
// nil interface instead of a typed nil.
func deliveryCommander(m *delivery.Manager) webapi.DeliveryCommander {
	if m == nil {
		return nil
	}
	return m
}

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
		// The manual review remote is assembled on its own: it is read-only and
		// never enables or stands in for automated delivery. A missing resolver
		// (no service credentials) leaves push review explicitly blocked while
		// local merge keeps working.
		manualRemote, remoteErr := manualRemoteRunner()
		if remoteErr != nil {
			return remoteErr
		}
		manager := agent.New(ctx, store, agent.Config{Program: wb.Program, DataDir: wb.DataDir, PiAgentDir: wb.PiAgentDir, Remote: manualRemote})
		defer func() { result = errors.Join(result, manager.Close()) }()
		previews := preview.New(ctx, store, preview.Config{DataDir: wb.DataDir, Reserve: manager.AcquirePreviewLease})
		manager.SetPreviewStopper(previews.StopTask)
		defer func() { result = errors.Join(result, previews.Close()) }()
		if err := manager.RecoverMerges(ctx); err != nil {
			return err
		}
		// Automated delivery is assembled inside the listening service's
		// lifecycle: recovery runs before any business traffic, and the worker
		// is closed after the API and before the agent and the store.
		deliveryMgr, err := buildDelivery(ctx, wb.DataDir, store, manager)
		if err != nil {
			return err
		}
		if deliveryMgr != nil {
			defer func() { result = errors.Join(result, deliveryMgr.Close()) }()
			if err := deliveryMgr.Recover(ctx); err != nil {
				return err
			}
		}
		catalog := &pimodel.Catalog{Program: wb.Program, SourceDir: wb.PiAgentDir, DataDir: filepath.Join(wb.DataDir, "probes")}
		handler = webapi.New(webapi.Config{DataDir: wb.DataDir, AcquireToolLease: manager.AcquireToolLease, Previews: previews, Models: catalog, Git: manager, Delivery: deliveryCommander(deliveryMgr), Context: ctx, Host: ln.Addr().String(), Token: wb.Token, Assets: webui.Files(), Health: handler, AgentAvailable: wb.Program != "", Version: buildinfo.Current(), Status: func() buildinfo.DeploymentStatus { return buildinfo.ReadDeploymentStatus(wb.DataDir) }}, store, manager)
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
