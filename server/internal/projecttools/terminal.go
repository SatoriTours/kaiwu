package projecttools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"os"
	"os/exec"
	"sync"
	"time"
)

type TerminalSnapshot struct {
	Offset  int64  `json:"offset"`
	ID      string `json:"id"`
	Scope   string `json:"scope"`
	Output  string `json:"output"`
	Running bool   `json:"running"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}
type terminal struct {
	TerminalSnapshot
	cmd     *exec.Cmd
	pty     *os.File
	done    chan struct{}
	changed chan struct{}
	lease   preview.Lease
	inputMu sync.Mutex
}
type Terminals struct {
	mu      sync.Mutex
	entries map[string]*terminal
	closed  bool
}

func NewTerminals(_ context.Context) *Terminals { return &Terminals{entries: map[string]*terminal{}} }
func (m *Terminals) Start(scope, dir string, lease preview.Lease) (TerminalSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	refuse := func(e error) (TerminalSnapshot, error) {
		if lease != nil {
			if l, ok := lease.(interface{ NoProcess() }); ok {
				l.NoProcess()
			}
			_ = lease.Release(true)
		}
		return TerminalSnapshot{}, e
	}
	if m.closed {
		return refuse(errors.New("终端服务已关闭"))
	}
	count := 0
	for _, t := range m.entries {
		if t.Running {
			count++
			if t.Scope == scope {
				return refuse(errors.New("此工作区已有终端，请接回原终端"))
			}
		}
	}
	if count >= 8 {
		return refuse(errors.New("最多 8 个终端"))
	}
	// Retain only a bounded number of ended terminal histories.
	if len(m.entries) >= 32 {
		for id, t := range m.entries {
			if !t.Running {
				delete(m.entries, id)
				break
			}
		}
	}
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return refuse(e)
	}
	t := &terminal{TerminalSnapshot: TerminalSnapshot{ID: hex.EncodeToString(id), Scope: scope, Running: true, Cols: 100, Rows: 30}, done: make(chan struct{}), changed: make(chan struct{}), lease: lease}
	cmd, pty, e := startPTY(dir, lease)
	if e != nil {
		return refuse(e)
	}
	t.cmd, t.pty = cmd, pty
	m.entries[t.ID] = t
	go m.consume(t)
	return t.TerminalSnapshot, nil
}
func (m *Terminals) consume(t *terminal) {
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 8192)
		for {
			n, e := t.pty.Read(buf)
			if n > 0 {
				m.mu.Lock()
				t.Output += string(buf[:n])
				t.Offset += int64(n)
				if len(t.Output) > 256<<10 {
					t.Output = t.Output[len(t.Output)-(256<<10):]
				}
				close(t.changed)
				t.changed = make(chan struct{})
				m.mu.Unlock()
			}
			if e != nil {
				break
			}
		}
	}()
	_ = t.cmd.Wait()
	safe := stopGroup(t.cmd.Process.Pid)
	_ = t.pty.Close()
	<-readerDone
	if t.lease != nil {
		_ = t.lease.Release(safe)
	}
	m.mu.Lock()
	t.Running = false
	close(t.changed)
	t.changed = make(chan struct{})
	close(t.done)
	m.mu.Unlock()
}
func (m *Terminals) Read(id string) (TerminalSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.entries[id]
	if t == nil {
		return TerminalSnapshot{}, errors.New("终端不存在或已随服务器重启结束")
	}
	return t.TerminalSnapshot, nil
}
func (m *Terminals) List(scope string) []TerminalSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []TerminalSnapshot{}
	for _, t := range m.entries {
		if t.Scope == scope {
			s := t.TerminalSnapshot
			s.Output = ""
			out = append(out, s)
		}
	}
	return out
}
func (m *Terminals) Changed(id string) (<-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.entries[id]; t != nil {
		return t.changed, nil
	}
	return nil, errors.New("终端不存在")
}
func (m *Terminals) Input(id, input string) error {
	if len(input) > 4096 {
		return errors.New("终端输入过长")
	}
	m.mu.Lock()
	t := m.entries[id]
	if t == nil || !t.Running {
		m.mu.Unlock()
		return errors.New("终端已结束")
	}
	f := t.pty
	m.mu.Unlock()
	t.inputMu.Lock()
	defer t.inputMu.Unlock()
	_ = f.SetWriteDeadline(time.Now().Add(time.Second))
	_, e := f.WriteString(input)
	return e
}

// Resize changes the actual PTY, so readline and foreground applications see
// the same geometry as the browser. Bound dimensions before uint16 conversion.
func (m *Terminals) Resize(id string, cols, rows int) error {
	if cols < 2 || cols > 1000 || rows < 1 || rows > 500 {
		return errors.New("终端尺寸无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.entries[id]
	if t == nil || !t.Running {
		return errors.New("终端已结束")
	}
	if cols == t.Cols && rows == t.Rows {
		return nil
	}
	if err := resizePTY(t.pty, cols, rows); err != nil {
		return err
	}
	t.Cols, t.Rows = cols, rows
	return nil
}
func (m *Terminals) Stop(id string) error {
	m.mu.Lock()
	t := m.entries[id]
	m.mu.Unlock()
	if t == nil {
		return errors.New("终端不存在")
	}
	if t.cmd.Process != nil {
		_ = signalGroup(t.cmd.Process.Pid)
	}
	select {
	case <-t.done:
		return nil
	case <-time.After(4 * time.Second):
		return errors.New("终端尚未确认退出，工作区锁仍保留")
	}
}
func (m *Terminals) Close() error {
	m.mu.Lock()
	m.closed = true
	ids := []string{}
	for id, t := range m.entries {
		if t.Running {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	var err error
	for _, id := range ids {
		err = errors.Join(err, m.Stop(id))
	}
	return err
}

// TerminalFrame is a bounded delta, or a full reset when a reconnect/slow reader
// has no usable cursor. Offsets are per process and never replay terminal input.
type TerminalFrame struct {
	TerminalSnapshot
	Reset bool `json:"reset"`
}

func (m *Terminals) ReadSince(id string, offset int64) (TerminalFrame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.entries[id]
	if t == nil {
		return TerminalFrame{}, errors.New("终端不存在")
	}
	frame := TerminalFrame{TerminalSnapshot: t.TerminalSnapshot}
	start := t.Offset - int64(len(t.Output))
	if offset < start || offset > t.Offset {
		frame.Reset = true
	} else {
		frame.Output = t.Output[offset-start:]
	}
	return frame, nil
}
