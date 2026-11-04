package aiconfig

import (
	"os"
	"syscall"
)

func writeLock(r *os.Root) (func(), error) {
	if info, err := r.Lstat(".write.lock"); err == nil && !info.Mode().IsRegular() {
		return nil, ErrConflict
	}
	f, err := r.OpenFile(".write.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrConflict
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
