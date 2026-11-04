//go:build !linux

package aiconfig

import (
	"errors"
	"os"
)

func writeLock(*os.Root) (func(), error) {
	return nil, errors.New("AI configuration writes require supported file locking")
}
