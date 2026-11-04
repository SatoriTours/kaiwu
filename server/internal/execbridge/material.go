package execbridge

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

// PublicBootstrap contains no private key, credentials, task argv or environment.
// Only this dedicated public directory may be mounted into a managed unit.
type PublicBootstrap struct {
	SchemaVersion int
	Bootstrap     Bootstrap
	Socket        string
}
type RunMaterial struct {
	mu               sync.Mutex
	run, public, key *os.File
	bootstrap        PublicBootstrap
	directory        FileIdentity
	closed           bool
}

func (m *RunMaterial) Public() PublicBootstrap {
	b := m.bootstrap
	b.Bootstrap.HostPublicKey = append(ed25519.PublicKey(nil), b.Bootstrap.HostPublicKey...)
	return b
}
func (m *RunMaterial) DirectoryIdentity() FileIdentity { return m.directory }
func (m *RunMaterial) SigningKey() (ed25519.PrivateKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.key == nil {
		return nil, executionenv.ErrUnavailable
	}
	info, err := m.key.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != ed25519.PrivateKeySize {
		return nil, executionenv.ErrUnavailable
	}
	data := make([]byte, ed25519.PrivateKeySize)
	if _, err := m.key.ReadAt(data, 0); err != nil || !bytes.Equal(ed25519.NewKeyFromSeed(data[:32]), data) || !bytes.Equal(data[32:], m.bootstrap.Bootstrap.HostPublicKey) {
		clear(data)
		return nil, executionenv.ErrUnavailable
	}
	return ed25519.PrivateKey(data), nil
}
func (m *RunMaterial) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		for _, f := range []*os.File{m.key, m.public, m.run} {
			if f != nil {
				f.Close()
			}
		}
	}
	return nil
}
func strictBootstrapJSON(data []byte, out any) error {
	if len(data) == 0 || len(data) > 16384 || !utf8.Valid(data) {
		return ErrBundle
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if uniqueManifestValue(check, 0) != nil {
		return ErrBundle
	}
	if _, err := check.Token(); err != io.EOF {
		return ErrBundle
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrBundle
	}
	return nil
}
func decodePublicBootstrap(data []byte) (PublicBootstrap, error) {
	var b PublicBootstrap
	if strictBootstrapJSON(data, &b) != nil || b.SchemaVersion != 1 || b.Socket != "control.sock" || !validBootstrap(b.Bootstrap) {
		return PublicBootstrap{}, ErrBundle
	}
	return b, nil
}
