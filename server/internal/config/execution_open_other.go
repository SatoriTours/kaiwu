//go:build !unix

package config

import (
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"os"
	"path/filepath"
)

func openExecutionRoot(name string) (*os.File, error) {
	// An absent policy remains absent for legacy installations. Existing policies
	// cannot be consumed on a platform without a verified no-follow reader.
	if _, err := os.Lstat(filepath.Join(name, "sandbox.json")); os.IsNotExist(err) {
		return nil, err
	}
	return nil, executionenv.ErrUnavailable
}
func openExecutionFile(*os.File) (*os.File, error) { return nil, executionenv.ErrUnavailable }
