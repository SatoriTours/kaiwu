//go:build !linux

package preview

import (
	"errors"
	"os"
)

// openStaticAt refuses static previews on platforms without the openat walk.
// A plain Open would not be race-free against symlink swaps, so this slice
// reports the platform limitation instead of serving through an unsafe open.
func openStaticAt(tree string, dirs []string, leaf string) (*os.File, os.FileInfo, error) {
	return nil, nil, errors.New("当前平台尚不支持静态预览")
}
