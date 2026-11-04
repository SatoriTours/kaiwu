//go:build linux

package execbridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

type materialOwner struct {
	SchemaVersion int
	Identity      executionenv.Identity
	SpecHash      string
}
type materialReady struct {
	SchemaVersion int
	PublicHash    string
	Directory     FileIdentity
}

// PrepareRunMaterial publishes immutable per-run material below an existing,
// private runtime root. Callers must already reserve the runtime in the journal.
// This writes no task directory and authorizes no unit/process start. Interrupted
// exclusive writes are preserved and rejected, not regenerated or overwritten.
func PrepareRunMaterial(ctx context.Context, root string, id executionenv.Identity, hash string) (out *RunMaterial, err error) {
	seed := Bootstrap{Identity: id, SpecHash: hash, HostPublicKey: make([]byte, ed25519.PublicKeySize)}
	if !validBootstrap(seed) || !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return nil, executionenv.ErrInvalid
	}
	parent, err := openMaterialPath(ctx, root)
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	defer parent.Close()
	if privateMaterialDirectory(parent) != nil || unix.Flock(int(parent.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return nil, executionenv.ErrUnavailable
	}
	defer unix.Flock(int(parent.Fd()), unix.LOCK_UN)
	created := false
	err = unix.Mkdirat(int(parent.Fd()), id.RunID, 0700)
	if err == nil {
		created = true
	} else if !errors.Is(err, unix.EEXIST) {
		return nil, executionenv.ErrUnavailable
	}
	run, err := openMaterialDirectory(parent, id.RunID)
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	m := &RunMaterial{run: run}
	defer func() {
		if out == nil {
			m.Close()
		}
	}()
	if privateMaterialDirectory(run) != nil {
		return nil, executionenv.ErrUnavailable
	}
	owner := materialOwner{SchemaVersion: 1, Identity: id, SpecHash: hash}
	ownerRaw, _ := json.Marshal(owner)
	if created {
		if writeMaterialFile(ctx, run, "owner.json", ownerRaw, 0600) != nil || parent.Sync() != nil {
			return nil, executionenv.ErrUnavailable
		}
	}
	ownerFile, data, err := readMaterialFile(ctx, run, "owner.json", 16384, 0600)
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	ownerFile.Close()
	var saved materialOwner
	if strictBootstrapJSON(data, &saved) != nil || saved != owner {
		return nil, executionenv.ErrUnavailable
	}
	keyFile, key, err := readMaterialFile(ctx, run, "host.key", ed25519.PrivateKeySize, 0600)
	if errors.Is(err, unix.ENOENT) {
		// Never regenerate a missing key if any published public state exists.
		for _, name := range []string{"public", "ready"} {
			var stat unix.Stat_t
			if e := unix.Fstatat(int(run.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(e, unix.ENOENT) {
				return nil, executionenv.ErrUnavailable
			}
		}
		_, generated, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, executionenv.ErrUnavailable
		}
		defer clear(generated)
		if writeMaterialFile(ctx, run, "host.key", generated, 0600) != nil {
			return nil, executionenv.ErrUnavailable
		}
		keyFile, key, err = readMaterialFile(ctx, run, "host.key", ed25519.PrivateKeySize, 0600)
	}
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	m.key = keyFile
	defer clear(key)
	if len(key) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.NewKeyFromSeed(key[:32]), key) {
		return nil, executionenv.ErrUnavailable
	}
	seed.HostPublicKey = append(ed25519.PublicKey(nil), key[32:]...)
	public := PublicBootstrap{SchemaVersion: 1, Bootstrap: seed, Socket: "control.sock"}
	encoded, _ := json.Marshal(public)

	marker, old, markerErr := readMaterialFile(ctx, run, "ready", 1024, 0600)
	published := markerErr == nil
	var savedReady materialReady
	if published {
		marker.Close()
		if strictBootstrapJSON(old, &savedReady) != nil || savedReady.SchemaVersion != 1 || savedReady.Directory.Inode == 0 {
			return nil, executionenv.ErrUnavailable
		}
	} else if !errors.Is(markerErr, unix.ENOENT) {
		return nil, executionenv.ErrUnavailable
	}
	if !published {
		if err = unix.Mkdirat(int(run.Fd()), "public", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, executionenv.ErrUnavailable
		}
	}
	m.public, err = openMaterialDirectory(run, "public")
	if err != nil || privateMaterialDirectory(m.public) != nil {
		return nil, executionenv.ErrUnavailable
	}
	pubFile, pubRaw, err := readMaterialFile(ctx, m.public, "public.json", 16384, 0444)
	if errors.Is(err, unix.ENOENT) && !published {
		if writeMaterialFile(ctx, m.public, "public.json", encoded, 0444) != nil {
			return nil, executionenv.ErrUnavailable
		}
		pubFile, pubRaw, err = readMaterialFile(ctx, m.public, "public.json", 16384, 0444)
	}
	if err != nil {
		return nil, executionenv.ErrUnavailable
	}
	pubFile.Close()
	decoded, err := decodePublicBootstrap(pubRaw)
	if err != nil || decoded.Bootstrap.Identity != id || decoded.Bootstrap.SpecHash != hash || !bytes.Equal(decoded.Bootstrap.HostPublicKey, seed.HostPublicKey) {
		return nil, executionenv.ErrUnavailable
	}

	entries, readErr := m.public.ReadDir(3)
	if readErr != nil && readErr != io.EOF {
		return nil, executionenv.ErrUnavailable
	}
	if len(entries) > 2 {
		return nil, executionenv.ErrUnavailable
	}
	for _, entry := range entries {
		if entry.Name() != "public.json" && entry.Name() != "control.sock" {
			return nil, executionenv.ErrUnavailable
		}
		if entry.Name() == "control.sock" && entry.Type()&os.ModeSocket == 0 {
			return nil, executionenv.ErrUnavailable
		}
	}
	var publicStat unix.Stat_t
	if unix.Fstat(int(m.public.Fd()), &publicStat) != nil {
		return nil, executionenv.ErrUnavailable
	}
	directory := FileIdentity{Device: uint64(publicStat.Dev), Inode: publicStat.Ino}
	sum := sha256.Sum256(pubRaw)
	ready := materialReady{SchemaVersion: 1, PublicHash: hex.EncodeToString(sum[:]), Directory: directory}
	if published {
		if savedReady != ready {
			return nil, executionenv.ErrUnavailable
		}
	} else {
		data, _ := json.Marshal(ready)
		if m.public.Sync() != nil || writeMaterialFile(ctx, run, "ready", data, 0600) != nil {
			return nil, executionenv.ErrUnavailable
		}
	}
	if run.Sync() != nil || parent.Sync() != nil || ctx.Err() != nil {
		return nil, executionenv.ErrUnavailable
	}
	var stat unix.Stat_t
	if unix.Fstat(int(m.public.Fd()), &stat) != nil {
		return nil, executionenv.ErrUnavailable
	}
	m.bootstrap = decoded
	m.directory = FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
	return m, nil
}
func openMaterialPath(ctx context.Context, absolute string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	fd, err := unix.Open("/", flags, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if ctx.Err() != nil || part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return nil, executionenv.ErrUnavailable
		}
		next, e := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), "runtime-directory"), nil
}
func openMaterialDirectory(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "runtime-directory"), nil
}
func privateMaterialDirectory(f *os.File) error {
	var stat unix.Stat_t
	if f == nil || unix.Fstat(int(f.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0777 != 0700 {
		return executionenv.ErrUnavailable
	}
	return nil
}
func readMaterialFile(ctx context.Context, dir *os.File, name string, limit int, mode uint32) (*os.File, []byte, error) {
	if ctx.Err() != nil {
		return nil, nil, executionenv.ErrUnavailable
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), "runtime-material")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != mode || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		f.Close()
		return nil, nil, executionenv.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit || ctx.Err() != nil {
		f.Close()
		clear(data)
		return nil, nil, executionenv.ErrUnavailable
	}
	return f, data, nil
}
func writeMaterialFile(ctx context.Context, dir *os.File, name string, data []byte, mode uint32) error {
	if ctx.Err() != nil {
		return executionenv.ErrUnavailable
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return executionenv.ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "runtime-material")
	defer f.Close()
	// Fresh exclusive file only: make public/private modes independent of the
	// daemon's (usually 0077) umask without changing process-global umask.
	if unix.Fchmod(fd, mode) != nil {
		return executionenv.ErrUnavailable
	}
	if writeFrame(f, data) != nil || f.Sync() != nil || dir.Sync() != nil || ctx.Err() != nil {
		return executionenv.ErrUnavailable
	}
	return nil
}
