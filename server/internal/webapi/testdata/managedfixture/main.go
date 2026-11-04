// Local browser fixture: real temporary Git/SQLite, no Agent or model calls.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/webapi"
	"github.com/SatoriTours/kaiwu/server/internal/webui"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func main() {
	root := flag.String("root", "", "temporary test root")
	flag.Parse()
	if *root == "" {
		panic("root required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := workbench.Open(filepath.Join(*root, "state.db"), workbench.Profile{})
	if err != nil {
		panic(err)
	}
	defer store.Close()
	if err := store.ConfigureAI(filepath.Join(*root, "config")); err != nil {
		panic(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		panic(err)
	}
	bin := filepath.Join(*root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		panic(err)
	}
	failFile := filepath.Join(*root, "fail-next-git")
	script := "#!/bin/sh\nif [ -e " + strconv.Quote(failFile) + " ]; then rm -- " + strconv.Quote(failFile) + "; exit 42; fi\nexec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		panic(err)
	}
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		panic(err)
	}
	h := webapi.New(webapi.Config{DataDir: *root, Context: ctx, Host: "127.0.0.1:17335", Token: "kaiwu-managed-fixture-token-000000", ProjectsRoot: filepath.Join(*root, "projects"), ManagedProjects: true, Assets: webui.Files(), Health: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}, store, nil)
	defer h.(interface{ Close() error }).Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fixture/fail-next-git", func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(failFile, []byte("fail once"), 0600); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	mux.Handle("/", h)
	srv := &http.Server{Addr: "127.0.0.1:17335", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		bounded, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = srv.Shutdown(bounded)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
