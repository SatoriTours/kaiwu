package execbridge

import (
	"net"
	"os"
	"sync"
)

// ControlListener owns only the host-control lease, never the task writer lease.
// The listener also retains its own public-directory descriptor.
// Close does not unlink the durable socket entry or signal/stop any unit.
type ControlListener struct {
	listener  *net.UnixListener
	lock      *os.File
	directory *os.File
	once      sync.Once
	err       error
}

func (l *ControlListener) Close() error {
	l.once.Do(func() { l.err = l.listener.Close(); l.lock.Close(); l.directory.Close() })
	return l.err
}

func (l *ControlListener) Accept() (net.Conn, error) { return l.listener.Accept() }
func (l *ControlListener) Addr() net.Addr            { return l.listener.Addr() }
