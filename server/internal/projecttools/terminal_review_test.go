//go:build linux

package projecttools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalDeltaDoesNotResendHistory(t *testing.T) {
	m := NewTerminals(context.Background())
	m.entries["test"] = &terminal{TerminalSnapshot: TerminalSnapshot{ID: "test", Output: strings.Repeat("a", 256<<10), Offset: 256 << 10, Running: true}}
	initial, e := m.ReadSince("test", -1)
	if e != nil || !initial.Reset || len(initial.Output) != 256<<10 {
		t.Fatal(initial, e)
	}
	m.entries["test"].Output = strings.Repeat("a", (256<<10)-3) + "new"
	m.entries["test"].Offset += 3
	delta, e := m.ReadSince("test", initial.Offset)
	if e != nil || delta.Reset || delta.Output != "new" {
		t.Fatal(delta, e)
	}
	unchanged, _ := m.ReadSince("test", delta.Offset)
	if unchanged.Output != "" {
		t.Fatal("resent history")
	}
	gap, _ := m.ReadSince("test", 0)
	if !gap.Reset {
		t.Fatal("missing reset after buffer overrun")
	}
}
func TestInterruptedAttachmentStagingDoesNotPoisonUploads(t *testing.T) {
	a := Attachments{Dir: t.TempDir()}
	temp := filepath.Join(a.Dir, "upload-crash-residue")
	os.Mkdir(temp, 0700)
	os.WriteFile(filepath.Join(temp, "data"), []byte("partial"), 0600)
	if _, e := a.Save("conv", "new.txt", bytes.NewBufferString("new")); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(temp, "data")); e != nil || string(b) != "partial" {
		t.Fatal("deleted residue", e)
	}
}
