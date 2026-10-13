// Package webui holds the built browser UI. Run apps/web's build before packaging.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

func Files() fs.FS { sub, _ := fs.Sub(assets, "dist"); return sub }
