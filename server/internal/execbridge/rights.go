package execbridge

import (
	"errors"
	"os"
	"sync"
)

var ErrBundle = errors.New("bridge descriptor bundle rejected")

type FileIdentity struct{ Device, Inode uint64 }
type BundleExpectation struct {
	Config, Lease, Spool FileIdentity
	HasLease             bool
}
type Bundle struct {
	Files []*os.File
	once  sync.Once
}

func (b *Bundle) Close() {
	if b != nil {
		b.once.Do(func() {
			for _, f := range b.Files {
				f.Close()
			}
		})
	}
}
