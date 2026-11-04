//go:build !linux

package execbridge

import (
	"context"
	"net"
)

func (v NativeVerifier) Pin(context.Context, net.Conn) (PinnedPeer, error) { return nil, ErrAuth }
