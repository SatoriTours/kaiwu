//go:build linux

package projecttools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A real shell must see the browser's terminal type and geometry. Without
// these, readline/clear and applications cannot render in the actual viewport.
func TestTerminalXtermGeometryAndControl(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	term, err := m.Start("xterm", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resize, ok := any(m).(interface{ Resize(string, int, int) error })
	if !ok {
		t.Fatal("terminal cannot receive browser geometry")
	}
	for _, size := range [][2]int{{0, 24}, {80, 0}, {1001, 24}, {80, 501}, {-1, 20}} {
		if resize.Resize(term.ID, size[0], size[1]) == nil {
			t.Fatal("accepted invalid geometry", size)
		}
	}
	if err = resize.Resize(term.ID, 91, 27); err != nil {
		t.Fatal(err)
	}
	if err = m.Input(term.ID, "printf 'TYPE=%s\\n' \"$TERM\"; stty size\r"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := m.Read(term.ID)
		if strings.Contains(s.Output, "TYPE=xterm-256color\r\n") && strings.Contains(s.Output, "27 91\r\n") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := m.Read(term.ID)
	t.Fatalf("terminal type/size not applied: %q", s.Output)
}
