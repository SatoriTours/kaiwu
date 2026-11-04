package aiconfig

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
	"io"
	"os"
	"path/filepath"
)

var ErrConflict = errors.New("AI configuration version changed or write in progress")

func fileHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func readFile(r *os.Root, name string) ([]byte, error) {
	info, err := r.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return nil, errors.New("AI file is not a bounded regular file")
	}
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("AI file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if len(b) > 256<<10 {
		return nil, errors.New("AI file too large")
	}
	return b, err
}
func openDir(dir string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("absolute AI configuration directory required")
	}
	if create {
		if err := projectstorage.EnsureDir(dir); err != nil {
			return nil, err
		}
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if canonical != filepath.Clean(dir) {
		return nil, errors.New("AI directory contains symlinks")
	}
	return os.OpenRoot(dir)
}
func Read(dir string) (Config, string, error) {
	r, err := openDir(dir, false)
	if os.IsNotExist(err) {
		return Config{SchemaVersion: 1}, "", nil
	}
	if err != nil {
		return Config{}, "", err
	}
	defer r.Close()
	b, err := readFile(r, "ai.json")
	if os.IsNotExist(err) {
		return Config{SchemaVersion: 1}, "", nil
	}
	if err != nil {
		return Config{}, "", err
	}
	c, err := Decode(b)
	return c, fileHash(b), err
}
func Write(dir, expected string, c Config) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if _, err = Decode(b); err != nil {
		return "", err
	}
	r, err := openDir(dir, true)
	if err != nil {
		return "", err
	}
	defer r.Close()
	unlock, err := writeLock(r)
	if err != nil {
		return "", err
	}
	defer unlock()
	old, err := readFile(r, "ai.json")
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	version := ""
	if err == nil {
		version = fileHash(old)
	}
	if version != expected {
		return "", ErrConflict
	}
	if err = atomicFile(r, "ai.json", b, false); err != nil {
		return "", err
	}
	return fileHash(b), nil
}
func atomicFile(r *os.Root, name string, b []byte, exclusive bool) error {
	// Random staging name; no cleanup of anyone else's files.
	id := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return err
	}
	temp := ".write-" + hex.EncodeToString(id)
	f, err := r.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer r.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if exclusive {
		err = r.Link(temp, name)
	} else {
		err = r.Rename(temp, name)
	}
	if err != nil {
		return err
	}
	d, err := r.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Freeze(dir, id string, snapshot Resolved) error {
	if !projectstorage.ValidDirectoryName(id) {
		return errors.New("invalid snapshot identity")
	}
	if err := freezeSkills(dir, id, snapshot); err != nil {
		return err
	}
	r, err := openDir(dir, true)
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	old, err := readFile(r, id+".json")
	if err == nil {
		if !bytes.Equal(b, old) {
			return errors.New("immutable AI snapshot mismatch")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return atomicFile(r, id+".json", b, true)
}
