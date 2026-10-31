package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestDeliveryAssemblyRecognizesExplicitSSHAgent(t *testing.T) {
	for _, tc := range []struct {
		name, flag, socket, api string
		want                    bool
	}{
		{name: "authorized agent only", flag: "allowed", socket: "synthetic.sock", api: "synthetic-api", want: true},
		{name: "ambient agent only", socket: "synthetic.sock", api: "synthetic-api"},
		{name: "flag without socket", flag: "allowed", api: "synthetic-api"},
		{name: "agent without API", flag: "allowed", socket: "synthetic.sock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KAIWU_GITHUB_API_TOKEN", tc.api)
			t.Setenv("KAIWU_GITHUB_GIT_TOKEN", "")
			t.Setenv("KAIWU_GITHUB_SSH_KEY", "")
			t.Setenv("KAIWU_GITHUB_SSH_AGENT", tc.flag)
			t.Setenv("SSH_AUTH_SOCK", tc.socket)
			dir := t.TempDir()
			store, err := workbench.Open(filepath.Join(dir, "workbench.db"), workbench.Profile{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			port := agent.New(ctx, store, agent.Config{DataDir: dir})
			defer port.Close()
			worker, err := buildDelivery(ctx, dir, store, port)
			if err != nil {
				t.Fatal(err)
			}
			if worker != nil {
				defer worker.Close()
			}
			if (worker != nil) != tc.want {
				t.Fatalf("delivery assembled=%t, want %t", worker != nil, tc.want)
			}
		})
	}
}
