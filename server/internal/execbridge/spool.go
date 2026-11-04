package execbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"os"
	"strings"
	"sync"
)

var ErrGap = errors.New("bridge event stream incomplete")

type FrameKind byte

const (
	Stdout FrameKind = iota + 1
	Stderr
	ProcessExit
	InputAttempt
	InputReceipt
	Attached
	Started
	StartFailed
	LaunchAttempt
)
const (
	spoolHeader    = 48
	frameHeader    = 24
	maxFrameData   = 65536
	maxSpoolFrames = 65536
	maxSpoolBytes  = 64 << 20
)

type Frame struct {
	Sequence uint64
	Kind     FrameKind
	Data     []byte
}
type spoolStorage interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Stat() (os.FileInfo, error)
	Sync() error
}
type frameIndex struct {
	offset int64
	size   int
	kind   FrameKind
}

// Spool has one writer, owned only by a live bridge. All operations use offsets,
// never the shared OFD seek cursor. It never truncates, rotates or hides a gap.
// Reopening validates evidence; it does NOT authorize restarting a dead bridge.
type Spool struct {
	mu         sync.Mutex
	file       spoolStorage
	run        []byte
	limit, end int64
	frames     []frameIndex
	failed     bool
	notify     chan struct{}
}

func OpenSpool(f spoolStorage, run string, limit int64) (*Spool, error) {
	id, err := hex.DecodeString(run)
	if err != nil || len(id) != 16 || strings.ToLower(run) != run || limit < 128 || limit > maxSpoolBytes || f == nil {
		return nil, ErrGap
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrGap
	}
	s := &Spool{file: f, run: id, limit: limit, end: spoolHeader, notify: make(chan struct{})}
	if info.Size() == 0 {
		if s.mark(0) != nil {
			return nil, ErrGap
		}
		return s, nil
	}
	header := make([]byte, spoolHeader)
	if _, err = f.ReadAt(header, 0); err != nil || !bytes.Equal(header, s.header(0)) {
		return nil, ErrGap
	}
	for s.end < info.Size() {
		if len(s.frames) >= maxSpoolFrames {
			return nil, ErrGap
		}
		_, index, err := s.readFrame(s.end, uint64(len(s.frames)+1))
		if err != nil || s.end+int64(frameHeader+index.size) > info.Size() {
			return nil, ErrGap
		}
		s.frames = append(s.frames, index)
		s.end += int64(frameHeader + index.size)
	}
	if s.end != info.Size() {
		return nil, ErrGap
	}
	return s, nil
}
func (s *Spool) header(state byte) []byte {
	b := make([]byte, spoolHeader)
	copy(b, "KWSPOOL1")
	copy(b[8:24], s.run)
	binary.BigEndian.PutUint64(b[24:32], uint64(s.limit))
	b[32] = state
	binary.BigEndian.PutUint32(b[44:], crc32.ChecksumIEEE(b[:44]))
	return b
}
func (s *Spool) mark(state byte) error {
	b := s.header(state)
	n, err := s.file.WriteAt(b, 0)
	if err != nil || n != len(b) {
		return ErrGap
	}
	if s.file.Sync() != nil {
		return ErrGap
	}
	return nil
}
func (s *Spool) gap() error {
	s.failed = true
	close(s.notify)
	s.notify = make(chan struct{})
	_ = s.mark(2)
	return ErrGap
}
func validFrameKind(k FrameKind) bool { return k >= Stdout && k <= LaunchAttempt }
func (s *Spool) Append(kind FrameKind, data []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return 0, ErrGap
	}
	if !validFrameKind(kind) || len(data) > maxFrameData || len(s.frames) >= maxSpoolFrames || int64(frameHeader+len(data)) > s.limit-s.end {
		return 0, s.gap()
	}
	// A durable dirty header precedes data. A partial write / missing final clean
	// header is never accepted as a completed stream after recovery.
	if s.mark(1) != nil {
		return 0, s.gap()
	}
	seq := uint64(len(s.frames) + 1)
	b := make([]byte, frameHeader+len(data))
	copy(b, "KWE1")
	binary.BigEndian.PutUint64(b[4:12], seq)
	b[12] = byte(kind)
	binary.BigEndian.PutUint32(b[16:20], uint32(len(data)))
	copy(b[frameHeader:], data)
	checksum := crc32.NewIEEE()
	checksum.Write(b[:20])
	checksum.Write(data)
	binary.BigEndian.PutUint32(b[20:24], checksum.Sum32())
	n, err := s.file.WriteAt(b, s.end)
	if err != nil || n != len(b) || s.file.Sync() != nil {
		return 0, s.gap()
	}
	if s.mark(0) != nil {
		return 0, s.gap()
	}
	s.frames = append(s.frames, frameIndex{offset: s.end, size: len(data), kind: kind})
	s.end += int64(len(b))
	close(s.notify)
	s.notify = make(chan struct{})
	return seq, nil
}
func (s *Spool) readFrame(offset int64, sequence uint64) (Frame, frameIndex, error) {
	b := make([]byte, frameHeader)
	if offset < spoolHeader || offset > s.limit-frameHeader {
		return Frame{}, frameIndex{}, ErrGap
	}
	if _, err := s.file.ReadAt(b, offset); err != nil || string(b[:4]) != "KWE1" || binary.BigEndian.Uint64(b[4:12]) != sequence || !validFrameKind(FrameKind(b[12])) || b[13] != 0 || b[14] != 0 || b[15] != 0 {
		return Frame{}, frameIndex{}, ErrGap
	}
	size := int(binary.BigEndian.Uint32(b[16:20]))
	if size > maxFrameData || int64(size) > s.limit-offset-frameHeader {
		return Frame{}, frameIndex{}, ErrGap
	}
	payload := make([]byte, size)
	if size > 0 {
		if _, err := s.file.ReadAt(payload, offset+frameHeader); err != nil {
			return Frame{}, frameIndex{}, ErrGap
		}
	}
	checksum := crc32.NewIEEE()
	checksum.Write(b[:20])
	checksum.Write(payload)
	if checksum.Sum32() != binary.BigEndian.Uint32(b[20:]) {
		return Frame{}, frameIndex{}, ErrGap
	}
	return Frame{Sequence: sequence, Kind: FrameKind(b[12]), Data: payload}, frameIndex{offset: offset, size: size, kind: FrameKind(b[12])}, nil
}
func (s *Spool) ReadAfter(after uint64, maxFrames, maxBytes int) ([]Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed || after > uint64(len(s.frames)) || maxFrames <= 0 || maxFrames > 256 || maxBytes <= 0 || maxBytes > 1<<20 {
		return nil, ErrGap
	}
	out := []Frame{}
	used := 0
	for i := int(after); i < len(s.frames) && len(out) < maxFrames; i++ {
		index := s.frames[i]
		if index.size > maxBytes-used {
			if len(out) == 0 {
				return nil, ErrGap
			}
			break
		}
		frame, actual, err := s.readFrame(index.offset, uint64(i+1))
		if err != nil || actual != index {
			return nil, s.gap()
		}
		used += len(frame.Data)
		out = append(out, frame)
	}
	return out, nil
}
func (s *Spool) Last() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.frames)), s.failed
}

func (s *Spool) WaitAfter(ctx context.Context, after uint64) error {
	for {
		s.mu.Lock()
		last := uint64(len(s.frames))
		failed := s.failed
		changed := s.notify
		s.mu.Unlock()
		if failed || after > last {
			return ErrGap
		}
		if last > after {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (s *Spool) Fail() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.failed {
		_ = s.gap()
	}
}
