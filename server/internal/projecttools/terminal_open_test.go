//go:build linux

package projecttools

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"sync"
	"testing"
)

func TestTerminalEnsureConcurrentEntryDoesNotDuplicateShell(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	opener, ok := any(m).(interface {
		Open(string, string, string, bool, func() (preview.Lease, error)) (TerminalSnapshot, error)
	})
	if !ok {
		t.Fatal("atomic terminal entry is not supported")
	}
	dir := t.TempDir()
	var wg sync.WaitGroup
	ids := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := opener.Open("scope", dir, "", true, func() (preview.Lease, error) { return nil, nil })
			if e != nil {
				t.Error(e)
				return
			}
			ids <- s.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("entry created duplicate shells")
		}
		id = got
	}
	if len(m.List("scope")) != 1 {
		t.Fatal("entry did not retain exactly one shell")
	}
}

func TestTerminalEnsureAliasDoesNotCreateAnotherShell(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	dir := t.TempDir()
	acquire := func() (preview.Lease, error) { return nil, nil }
	first, e := m.Open("scope", dir, "initial", true, acquire)
	if e != nil {
		t.Fatal(e)
	}
	alias, e := m.Open("scope", dir, "other-tab", true, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if alias.ID != first.ID {
		t.Fatal("ensure alias created shell")
	}
	retry, e := m.Open("scope", dir, "other-tab", false, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if retry.ID != first.ID {
		t.Fatal("lost ensure reply created another shell")
	}
}
func TestTerminalOperationSurvivesHistoryEviction(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	dir := t.TempDir()
	acquire := func() (preview.Lease, error) { return nil, nil }
	first, e := m.Open("scope", dir, "old-op", false, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Stop(first.ID); e != nil {
		t.Fatal(e)
	}
	// Simulate bounded ended-history eviction without spawning 32 shells.
	m.mu.Lock()
	delete(m.entries, first.ID)
	m.mu.Unlock()
	replay, e := m.Open("scope", dir, "old-op", false, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if replay.ID != first.ID || replay.Running {
		t.Fatal("history eviction replayed launch")
	}
}
func TestTerminalLookupResolvesAliasesWithoutCreating(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	lookup, ok := any(m).(interface {
		Lookup(string, string) (TerminalSnapshot, bool)
	})
	if !ok {
		t.Fatal("read-only terminal operation lookup unavailable")
	}
	calls := 0
	acquire := func() (preview.Lease, error) { calls++; return nil, nil }
	dir := t.TempDir()
	first, e := m.Open("scope", dir, "first", true, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Open("scope", dir, "alias", true, acquire); e != nil {
		t.Fatal(e)
	}
	result, known := lookup.Lookup("scope", "alias")
	if !known || result.ID != first.ID || !result.Running {
		t.Fatal("alias not resolved")
	}
	if _, known = lookup.Lookup("other", "alias"); known {
		t.Fatal("operation crossed scope")
	}
	if _, known = lookup.Lookup("scope", "unknown"); known || calls != 1 {
		t.Fatal("lookup performed a launch")
	}
	if e = m.Stop(first.ID); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	delete(m.entries, first.ID)
	m.mu.Unlock()
	result, known = lookup.Lookup("scope", "alias")
	if !known || result.ID != first.ID || result.Running {
		t.Fatal("evicted result not resolved")
	}
}
func TestTerminalCreationOperationIsNotReplayedAfterStop(t *testing.T) {
	m := NewTerminals(context.Background())
	defer m.Close()
	opener, ok := any(m).(interface {
		Open(string, string, string, bool, func() (preview.Lease, error)) (TerminalSnapshot, error)
	})
	if !ok {
		t.Fatal("idempotent terminal creation is not supported")
	}
	dir := t.TempDir()
	calls := 0
	acquire := func() (preview.Lease, error) { calls++; return nil, nil }
	first, e := opener.Open("scope", dir, "create-once", false, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Stop(first.ID); e != nil {
		t.Fatal(e)
	}
	second, e := opener.Open("scope", dir, "create-once", false, acquire)
	if e != nil {
		t.Fatal(e)
	}
	if second.ID != first.ID || second.Running || calls != 1 {
		t.Fatal("lost response replay started a new shell")
	}
}
