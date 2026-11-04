package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedProjectRootDefaultsAndOverridesWithoutMovingData(t *testing.T) {
	data := t.TempDir()
	env := map[string]string{"KAIWU_WEB_TOKEN": strings.Repeat("x", 32), "KAIWU_DATA_DIR": data}
	load := func() (Workbench, error) {
		return LoadWorkbench(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	got, err := load()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectsRoot != filepath.Join(home, "projects") || got.DataDir != data {
		t.Fatalf("defaults changed data: %+v", got)
	}
	env["KAIWU_PROJECTS_ROOT"] = filepath.Join(t.TempDir(), "custom")
	got, err = load()
	if err != nil || got.ProjectsRoot != env["KAIWU_PROJECTS_ROOT"] || got.DataDir != data {
		t.Fatalf("override: %+v %v", got, err)
	}
	env["KAIWU_PROJECTS_ROOT"] = "relative"
	if _, err := load(); err == nil {
		t.Fatal("relative root accepted")
	}
}
