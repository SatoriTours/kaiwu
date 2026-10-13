package pirpc

import "sync"

type tailBuffer struct {
	mu        sync.Mutex
	data      []byte
	pos, size int
}

func newTailBuffer(maxBytes int) *tailBuffer { return &tailBuffer{data: make([]byte, maxBytes)} }
func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) >= len(b.data) {
		copy(b.data, p[len(p)-len(b.data):])
		b.pos = 0
		b.size = len(b.data)
		return n, nil
	}
	first := copy(b.data[b.pos:], p)
	copy(b.data, p[first:])
	b.pos = (b.pos + n) % len(b.data)
	b.size = min(len(b.data), b.size+n)
	return n, nil
}
func (b *tailBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.size)
	start := (b.pos - b.size + len(b.data)) % len(b.data)
	n := copy(out, b.data[start:])
	copy(out[n:], b.data[:b.pos])
	return out
}
