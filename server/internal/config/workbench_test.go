package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkbenchConfig(t *testing.T) {
	env := map[string]string{"KAIWU_WEB_TOKEN": strings.Repeat("x", 32), "KAIWU_DATA_DIR": filepath.Join(t.TempDir(), "state"), "KAIWU_PI_PROGRAM": "/usr/local/bin/pi", "KAIWU_PI_PROVIDER": "test", "KAIWU_PI_MODEL": "model"}
	wb, e := LoadWorkbench(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if e != nil || wb.Token == "" || wb.Provider != "test" {
		t.Fatalf("%+v %v", wb, e)
	}
	env["KAIWU_WEB_TOKEN"] = "short"
	if _, e = LoadWorkbench(func(k string) (string, bool) { v, ok := env[k]; return v, ok }); e == nil {
		t.Fatal("weak token accepted")
	}
	if wb, e = LoadWorkbench(func(string) (string, bool) { return "", false }); e != nil || wb.Token != "" {
		t.Fatal("health-only mode", e)
	}
}

func TestPiDetectionAndExplicitOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable permissions fixture")
	}
	bin := t.TempDir()
	program := filepath.Join(bin, "pi")
	if e := os.WriteFile(program, []byte("this fixture must never execute"), 0700); e != nil {
		t.Fatal(e)
	}
	env := map[string]string{"KAIWU_WEB_TOKEN": strings.Repeat("x", 32), "KAIWU_DATA_DIR": t.TempDir(), "PATH": "." + string(os.PathListSeparator) + bin}
	load := func() (Workbench, error) {
		return LoadWorkbench(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	}
	wb, e := load()
	if e != nil || wb.Program != program || wb.Model != "" {
		t.Fatalf("automatic detection without a model: %+v %v", wb, e)
	}
	env["KAIWU_PI_PROGRAM"] = "/explicit/pi"
	wb, e = load()
	if e != nil || wb.Program != "/explicit/pi" {
		t.Fatal("explicit path was overridden", e)
	}
	env["KAIWU_PI_PROGRAM"] = "pi"
	if _, e = load(); e == nil {
		t.Fatal("relative override accepted")
	}
	delete(env, "KAIWU_PI_PROGRAM")
	env["PATH"] = "."
	wb, e = load()
	if e != nil || wb.Program != "" {
		t.Fatal("relative PATH used", e)
	}
	env["PATH"] = bin
	if e = os.Chmod(program, 0600); e != nil {
		t.Fatal(e)
	}
	wb, e = load()
	if e != nil || wb.Program != "" {
		t.Fatal("non-executable pi detected", e)
	}
}
