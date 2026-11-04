//go:build linux

package execbridge

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

// LoadPublicBootstrap reads only the dedicated public directory after bridge
// hardening. The directory must be supplied by trusted startup code, not task
// argv. File modes are not evidence of a read-only mount: native attestation
// remains mandatory before PrepareSession or executing a task.
func LoadPublicBootstrap(ctx context.Context, directory string) (PublicBootstrap, error) {
	fail := func() (PublicBootstrap, error) { return PublicBootstrap{}, executionenv.ErrUnavailable }
	if ctx.Err() != nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" {
		return fail()
	}
	dir, err := openMaterialPath(ctx, directory)
	if err != nil {
		return fail()
	}
	defer dir.Close()
	if privateMaterialDirectory(dir) != nil {
		return fail()
	}
	entries, err := dir.ReadDir(3)
	if err != nil && err != io.EOF {
		return fail()
	}
	if len(entries) == 0 || len(entries) > 2 {
		return fail()
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "public.json":
		case "control.sock":
			if entry.Type()&os.ModeSocket == 0 {
				return fail()
			}
		default:
			return fail()
		}
	}
	file, data, err := readMaterialFile(ctx, dir, "public.json", 16384, 0444)
	if err != nil {
		return fail()
	}
	defer file.Close()
	b, err := decodePublicBootstrap(data)
	if err != nil {
		return fail()
	}
	// Retain pins until the entire read is finished, then reject changed path
	// bindings. This does not authorize any future pathname-based dial or mount.
	fresh, err := openMaterialPath(ctx, directory)
	if err != nil {
		return fail()
	}
	defer fresh.Close()
	var before, after, held, named unix.Stat_t
	if unix.Fstat(int(dir.Fd()), &before) != nil || unix.Fstat(int(fresh.Fd()), &after) != nil || before.Dev != after.Dev || before.Ino != after.Ino || unix.Fstat(int(file.Fd()), &held) != nil || unix.Fstatat(int(dir.Fd()), "public.json", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || held.Dev != named.Dev || held.Ino != named.Ino || ctx.Err() != nil {
		return fail()
	}
	return b, nil
}
