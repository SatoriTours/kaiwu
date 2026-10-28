//go:build linux

package projecttools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTerminalRealProcessReconnectAndStop(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	term, e := m.Start("scope", t.TempDir(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start("scope", t.TempDir(), nil); e == nil {
		t.Fatal("second writer")
	}
	if e = m.Input(term.ID, "printf 'KAIWU_TEST_OK\\n'\n"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	var s TerminalSnapshot
	for time.Now().Before(deadline) {
		s, e = m.Read(term.ID)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(s.Output, "KAIWU_TEST_OK\r\n") {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !strings.Contains(s.Output, "KAIWU_TEST_OK\r\n") {
		t.Fatal(s.Output)
	}
	again, _ := m.Read(term.ID)
	if again.Output != s.Output {
		t.Fatal("reconnect lost output")
	}
	if e = m.Stop(term.ID); e != nil {
		t.Fatal(e)
	}
	s, _ = m.Read(term.ID)
	if s.Running {
		t.Fatal("process still running")
	}
}
