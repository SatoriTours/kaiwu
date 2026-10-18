package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SatoriTours/kaiwu/server/internal/app"
	"github.com/SatoriTours/kaiwu/server/internal/config"
	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
)

func run(ctx context.Context, lookupEnv func(string) (string, bool), stderr io.Writer) int {
	// The hidden credential helper subcommand is routed before any
	// configuration is read: git invokes the server binary itself.
	if handled, err := githubauth.HandleCredentialCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	cfg, err := config.Load(lookupEnv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 1
	}
	cfg.Workbench, err = config.LoadWorkbench(lookupEnv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 1
	}
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("server failed", "error", err)
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stderr)
	stop()
	os.Exit(code)
}
