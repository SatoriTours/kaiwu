//go:build unix

package config

import (
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const executionJSON = `{"schemaVersion":1,"maxResidentCLI":8,"globalBudget":{"cpuQuotaUSec":100000,"memoryBytes":536870912,"pidsMax":128,"diskBytes":16384},"defaultBudget":{"cpuQuotaUSec":25000,"memoryBytes":100663296,"pidsMax":32,"diskBytes":4096},"toolchains":["/tools/pi","/usr/bin/git"]}`

func TestLoadExecutionMissingDoesNotCreateConfig(t *testing.T) {
	root := filepath.Join(executionTempDir(t), "absent")
	c, err := LoadExecution(root)
	if err != nil || c.Configured {
		t.Fatalf("missing config: %+v %v", c, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("loader created directory: %v", err)
	}
}
func TestLoadExecutionRequiresExplicitBoundedBudget(t *testing.T) {
	root := executionTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "sandbox.json"), []byte(executionJSON), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadExecution(root)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Configured || c.MaxResidentCLI != 8 || c.DefaultBudget.MemoryBytes != 100663296 || len(c.Toolchains) != 2 {
		t.Fatalf("configuration lost: %+v", c)
	}
}
func TestExecutionConfigRejectsAuthorityClaimsAndSecrets(t *testing.T) {
	cases := map[string]string{
		"unknown-schema":        strings.Replace(executionJSON, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"credential":            strings.TrimSuffix(executionJSON, "}") + `,"apiKey":"do-not-reflect"}`,
		"observation":           strings.TrimSuffix(executionJSON, "}") + `,"capabilities":{"diskVerified":true}}`,
		"duplicate-policy":      strings.TrimSuffix(executionJSON, "}") + `,"maxResidentCLI":7}`,
		"case-duplicate-policy": strings.TrimSuffix(executionJSON, "}") + `,"MAXRESIDENTCLI":7}`,
		"extra-object":          executionJSON + ` {"do-not-reflect":true}`,
		"secret-key":            `{"do-not-reflect":"secret"}`,
		"zero-budget":           strings.Replace(executionJSON, `"cpuQuotaUSec":25000`, `"cpuQuotaUSec":0`, 1),
		"oversubscribed":        strings.Replace(executionJSON, `"memoryBytes":100663296`, `"memoryBytes":999999999`, 1),
		"resident-expansion":    strings.Replace(executionJSON, `"maxResidentCLI":8`, `"maxResidentCLI":9`, 1),
		"relative-tool":         strings.Replace(executionJSON, `"/tools/pi"`, `"relative"`, 1),
		"all-home":              strings.Replace(executionJSON, `"/tools/pi"`, `"/home"`, 1),
		"no-toolchain":          strings.Replace(executionJSON, `["/tools/pi","/usr/bin/git"]`, `[]`, 1),
		"large":                 strings.Repeat(" ", 65537),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := executionTempDir(t)
			if err := os.WriteFile(filepath.Join(root, "sandbox.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := LoadExecution(root)
			if !errors.Is(err, executionenv.ErrInvalid) || c.Configured {
				t.Fatalf("unsafe config accepted: %+v %v", c, err)
			}
			if strings.Contains(err.Error(), "do-not-reflect") {
				t.Fatal("secret reflected")
			}
		})
	}
}
func TestExecutionConfigRejectsLinkedOrNonRegularFile(t *testing.T) {
	for _, kind := range []string{"link", "directory", "root-link"} {
		t.Run(kind, func(t *testing.T) {
			base := executionTempDir(t)
			root := filepath.Join(base, "config")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(base, "source")
			if err := os.WriteFile(target, []byte(executionJSON), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "link":
				err = os.Symlink(target, filepath.Join(root, "sandbox.json"))
			case "directory":
				err = os.Mkdir(filepath.Join(root, "sandbox.json"), 0700)
			case "root-link":
				real := root
				root = filepath.Join(base, "alias")
				err = os.Symlink(real, root)
			}
			if err != nil {
				t.Skipf("filesystem symlink capability: %v", err)
			}
			if _, err := LoadExecution(root); !errors.Is(err, executionenv.ErrInvalid) {
				t.Fatalf("unsafe material accepted: %v", err)
			}
		})
	}
}

func TestExecutionRejectsAncestorSymlink(t *testing.T) {
	base := executionTempDir(t)
	real := filepath.Join(base, "real")
	root := filepath.Join(real, "config")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sandbox.json"), []byte(executionJSON), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadExecution(filepath.Join(alias, "config")); !errors.Is(err, executionenv.ErrInvalid) || c.Configured {
		t.Fatalf("ancestor redirect accepted: configured=%t err=%v", c.Configured, err)
	}
}
func TestExecutionRejectsUnicodeSchemaAndBudgetOverrides(t *testing.T) {
	base := strings.Replace(executionJSON, `"schemaVersion":1`, `"schemaVersion":2`, 1)
	for i, body := range []string{
		strings.TrimSuffix(base, "}") + `,"ſchemaVersion":1}`,
		strings.TrimSuffix(base, "}") + `,"\u017fchemaVersion":1}`,
		strings.Replace(executionJSON, `"diskBytes":4096}`, `"diskBytes":99999999,"disKBytes":4096}`, 1),
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			root := executionTempDir(t)
			if err := os.WriteFile(filepath.Join(root, "sandbox.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if c, err := LoadExecution(root); !errors.Is(err, executionenv.ErrInvalid) || c.Configured {
				t.Fatalf("Unicode override accepted: configured=%t err=%v", c.Configured, err)
			}
		})
	}
}

func executionTempDir(t *testing.T) string {
	t.Helper()
	// On macOS the system's /var temp prefix itself is an alias. Tests needing a
	// normal directory use its canonical path; intentional link cases stay links.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestExecutionPinnedRootSurvivesAncestorReplacement(t *testing.T) {
	base := executionTempDir(t)
	parent := filepath.Join(base, "parent")
	root := filepath.Join(parent, "config")
	hostile := filepath.Join(base, "other")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hostile, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sandbox.json"), []byte(executionJSON), 0600); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(executionJSON, `"maxResidentCLI":8`, `"maxResidentCLI":7`, 1)
	if err := os.WriteFile(filepath.Join(hostile, "config", "sandbox.json"), []byte(other), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := openExecutionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := os.Rename(parent, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hostile, parent); err != nil {
		t.Fatal(err)
	}
	f, err := openExecutionFile(handle)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != executionJSON {
		t.Fatal("pinned descriptor followed rebound ancestor")
	}
	if c, err := LoadExecution(root); err == nil || c.Configured {
		t.Fatal("replacement ancestor link admitted")
	}
}
func TestExecutionRejectsFinalReplacementAndFIFO(t *testing.T) {
	root := executionTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "original"), []byte(executionJSON), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := openExecutionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := os.Symlink("original", filepath.Join(root, "sandbox.json")); err != nil {
		t.Fatal(err)
	}
	if f, err := openExecutionFile(handle); err == nil {
		f.Close()
		t.Fatal("final link followed")
	}
	if err := os.Remove(filepath.Join(root, "sandbox.json")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "sandbox.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadExecution(root); !errors.Is(err, executionenv.ErrInvalid) || c.Configured {
		t.Fatal("special file accepted")
	}
}
