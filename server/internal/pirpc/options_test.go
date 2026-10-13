package pirpc

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOptionsDefaults(t *testing.T) {
	got, err := normalizeOptions(Options{})
	want := Options{MaxRecordBytes: 4 << 20, MaxPending: 32, MaxWriteBytes: 16 << 20, MaxEvents: 128, MaxEventBytes: 16 << 20, StderrTailBytes: 64 << 10, StartupTimeout: 8 * time.Second, CallTimeout: 30 * time.Second, WriteTimeout: 2 * time.Second, ShutdownTimeout: 5 * time.Second, DrainTimeout: time.Second}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults=%+v err=%v", got, err)
	}
}
func TestOptionsOverridesAndRejectsNegative(t *testing.T) {
	typ := reflect.TypeOf(Options{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Name == "Logger" {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			var opts Options
			reflect.ValueOf(&opts).Elem().Field(i).SetInt(7)
			got, err := normalizeOptions(opts)
			if err != nil || reflect.ValueOf(got).Field(i).Int() != 7 {
				t.Fatalf("override: %+v %v", got, err)
			}
			reflect.ValueOf(&opts).Elem().Field(i).SetInt(-1)
			if _, err = normalizeOptions(opts); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("negative: %v", err)
			}
		})
	}
}
func TestLaunchRequiresExplicitEnvironment(t *testing.T) {
	dir := t.TempDir()
	valid := LaunchSpec{Program: filepath.Join(dir, "helper"), Dir: dir, Env: []string{}}
	if err := validateLaunch(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*LaunchSpec)
	}{
		{"nil_env", func(s *LaunchSpec) { s.Env = nil }}, {"relative_program", func(s *LaunchSpec) { s.Program = "pi" }},
		{"relative_dir", func(s *LaunchSpec) { s.Dir = "." }}, {"nul_argument", func(s *LaunchSpec) { s.Args = []string{"x\x00y"} }},
		{"bad_env", func(s *LaunchSpec) { s.Env = []string{"NO_EQUALS"} }}, {"empty_key", func(s *LaunchSpec) { s.Env = []string{"=value"} }},
		{"nul_env", func(s *LaunchSpec) { s.Env = []string{"X=a\x00b"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.change(&s)
			if err := validateLaunch(s); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
