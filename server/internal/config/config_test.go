package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	cfg, err := Load(func(string) (string, bool) { return "", false })
	if err != nil || cfg.ListenAddr != "127.0.0.1:7331" {
		t.Fatalf("Load() = %+v, %v", cfg, err)
	}
}

func TestLANRequiresAuthenticatedWorkbench(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		address, token string
		allowed        bool
	}{
		{"192.168.2.122:7331", strings.Repeat("x", 32), true},
		{"192.168.2.122:7331", "", false},
		{"192.168.2.122:7331", "short", false},
		{"0.0.0.0:7331", strings.Repeat("x", 32), false},
		{"8.8.8.8:7331", strings.Repeat("x", 32), false},
	} {
		t.Run(tc.address+tc.token[:min(len(tc.token), 1)], func(t *testing.T) {
			env := map[string]string{"KAIWU_LISTEN_ADDR": tc.address, "KAIWU_WEB_TOKEN": tc.token, "KAIWU_DATA_DIR": filepath.Join(root, "state")}
			cfg, e := Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
			if (e == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, e)
			}
			direct := Config{ListenAddr: tc.address, Workbench: Workbench{Token: tc.token, DataDir: root}}
			if (direct.Validate() == nil) != tc.allowed {
				t.Fatal("direct config bypassed LAN authentication")
			}
			if e == nil && cfg.Workbench.Token != tc.token {
				t.Fatal("LAN configuration lost authentication")
			}
		})
	}
}

func TestLoadLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7332", "127.0.0.2:1", "[::1]:65535"} {
		t.Run(addr, func(t *testing.T) {
			cfg, err := Load(func(key string) (string, bool) {
				if key != "KAIWU_LISTEN_ADDR" {
					t.Fatalf("unexpected key: %s", key)
				}
				return addr, true
			})
			if err != nil || cfg.ListenAddr != addr {
				t.Fatalf("Load() = %+v, %v", cfg, err)
			}
		})
	}
}

func TestLoadRejectsUnsafeAddress(t *testing.T) {
	for _, addr := range []string{
		"", ":7331", "0.0.0.0:7331", "192.168.1.2:7331", "localhost:7331",
		"[::]:7331", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:+7331",
		"127.0.0.1:-1", "127.0.0.1:http", "127.0.0.1:", "127.0.0.1",
		" 127.0.0.1:7331", "127.0.0.1:7331 ", "[::ffff:127.0.0.1]:7331",
		"[::1%lo]:7331", "secret-value:7331",
	} {
		t.Run(addr, func(t *testing.T) {
			_, err := Load(func(string) (string, bool) { return addr, true })
			if err == nil {
				t.Fatalf("accepted unsafe address %q", addr)
			}
			if !strings.Contains(err.Error(), "KAIWU_LISTEN_ADDR") {
				t.Fatalf("error lacks configuration key: %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("error leaked raw configuration: %v", err)
			}
		})
	}
}

func TestValidateDirectConfig(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:7331", "remote.example:7331", ""} {
		if err := (Config{ListenAddr: addr}).Validate(); err == nil {
			t.Fatalf("accepted direct unsafe config: %q", addr)
		}
	}
}
