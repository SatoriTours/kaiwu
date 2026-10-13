package pirpc

import (
	"path/filepath"
	"strings"
	"time"
)

func normalizeOptions(o Options) (Options, error) {
	ints := []*int{&o.MaxRecordBytes, &o.MaxPending, &o.MaxWriteBytes, &o.MaxEvents, &o.MaxEventBytes, &o.StderrTailBytes}
	defaults := []int{4 << 20, 32, 16 << 20, 128, 16 << 20, 64 << 10}
	for i, p := range ints {
		if *p < 0 {
			return Options{}, ErrInvalidInput
		}
		if *p == 0 {
			*p = defaults[i]
		}
	}
	times := []*time.Duration{&o.StartupTimeout, &o.CallTimeout, &o.WriteTimeout, &o.ShutdownTimeout, &o.DrainTimeout}
	td := []time.Duration{8 * time.Second, 30 * time.Second, 2 * time.Second, 5 * time.Second, time.Second}
	for i, p := range times {
		if *p < 0 {
			return Options{}, ErrInvalidInput
		}
		if *p == 0 {
			*p = td[i]
		}
	}
	return o, nil
}
func validateLaunch(s LaunchSpec) error {
	if !filepath.IsAbs(s.Program) || !filepath.IsAbs(s.Dir) || s.Env == nil || strings.ContainsRune(s.Program, 0) || strings.ContainsRune(s.Dir, 0) {
		return ErrInvalidInput
	}
	for _, arg := range s.Args {
		if strings.ContainsRune(arg, 0) {
			return ErrInvalidInput
		}
	}
	for _, v := range s.Env {
		k, _, ok := strings.Cut(v, "=")
		if !ok || k == "" || strings.ContainsRune(v, 0) {
			return ErrInvalidInput
		}
	}
	return nil
}
