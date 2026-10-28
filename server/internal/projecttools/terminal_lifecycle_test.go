//go:build linux

package projecttools

import (
	"context"
	"testing"
	"time"
)

func TestTerminalExitReapsBackgroundAndNotifiesAll(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	s, e := m.Start("scope", t.TempDir(), nil)
	if e != nil {
		t.Fatal(e)
	}
	first, _ := m.Changed(s.ID)
	second, _ := m.Changed(s.ID)
	if e = m.Input(s.ID, "sleep 5 & exit\n"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first subscriber not notified")
	}
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("second subscriber not notified")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, _ = m.Read(s.ID)
		if !s.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("exited shell still waits for background child's PTY")
}
