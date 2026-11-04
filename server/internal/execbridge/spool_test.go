package execbridge

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func spoolFixture(t *testing.T, limit int64) (*Spool, *os.File) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "spool")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	s, err := OpenSpool(f, strings.Repeat("1", 32), limit)
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}
func TestSpoolDurableSequenceAndBoundedRead(t *testing.T) {
	s, f := spoolFixture(t, 4096)
	for _, b := range []string{"one", "two", "three"} {
		if _, err := s.Append(Stdout, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := s.ReadAfter(1, 1, 128)
	if err != nil || len(frames) != 1 || frames[0].Sequence != 2 || string(frames[0].Data) != "two" {
		t.Fatalf("bad bounded replay: %v %v", frames, err)
	}
	reopened, err := OpenSpool(f, strings.Repeat("1", 32), 4096)
	if err != nil {
		t.Fatal(err)
	}
	frames, err = reopened.ReadAfter(0, 10, 1024)
	if err != nil || len(frames) != 3 {
		t.Fatalf("durable stream lost: %v %v", frames, err)
	}
	if _, err := reopened.ReadAfter(4, 10, 1024); !errors.Is(err, ErrGap) {
		t.Fatal("future watermark silently accepted")
	}
}
func TestBridgeOverflowCannotInventSuccess(t *testing.T) {
	s, f := spoolFixture(t, 256)
	if _, err := s.Append(Stdout, bytes.Repeat([]byte("x"), 200)); !errors.Is(err, ErrGap) {
		t.Fatal("overflow accepted")
	}
	if _, err := s.Append(ProcessExit, []byte{0, 0, 0, 0}); !errors.Is(err, ErrGap) {
		t.Fatal("overflow invented success")
	}
	if st, err := f.Stat(); err != nil || st.Size() > 256 {
		t.Fatal("physical spool grew past budget")
	}
	if _, err := OpenSpool(f, strings.Repeat("1", 32), 256); !errors.Is(err, ErrGap) {
		t.Fatal("gap forgotten on reopen")
	}
}
func TestSpoolCorruptionAndForeignRunFailClosed(t *testing.T) {
	for _, where := range []int64{0, 32, 48 + 20, 48 + 24} {
		t.Run(string(rune('A'+where)), func(t *testing.T) {
			s, f := spoolFixture(t, 4096)
			if _, err := s.Append(Stdout, []byte("test")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt([]byte{255}, where); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenSpool(f, strings.Repeat("1", 32), 4096); !errors.Is(err, ErrGap) {
				t.Fatal("corruption accepted")
			}
		})
	}
	_, f := spoolFixture(t, 4096)
	if _, err := OpenSpool(f, strings.Repeat("2", 32), 4096); !errors.Is(err, ErrGap) {
		t.Fatal("foreign spool accepted")
	}
}

type brokenSpoolFile struct {
	*os.File
	short, failSync bool
}

func (f *brokenSpoolFile) WriteAt(p []byte, o int64) (int, error) {
	if f.short && o >= 48 {
		return f.File.WriteAt(p[:len(p)/2], o)
	}
	return f.File.WriteAt(p, o)
}
func (f *brokenSpoolFile) Sync() error {
	if f.failSync {
		return os.ErrPermission
	}
	return f.File.Sync()
}
func TestSpoolPartialWriteAndSyncFailureAreSticky(t *testing.T) {
	for _, name := range []string{"short", "sync"} {
		t.Run(name, func(t *testing.T) {
			_, file := spoolFixture(t, 4096)
			fault := &brokenSpoolFile{File: file}
			s, err := OpenSpool(fault, strings.Repeat("1", 32), 4096)
			if err != nil {
				t.Fatal(err)
			}
			fault.short = name == "short"
			fault.failSync = name == "sync"
			if _, err := s.Append(Stdout, []byte("unconfirmed")); !errors.Is(err, ErrGap) {
				t.Fatal("I/O fault accepted")
			}
			fault.short = false
			fault.failSync = false
			if _, err := s.Append(ProcessExit, []byte{0, 0, 0, 0}); !errors.Is(err, ErrGap) {
				t.Fatal("I/O fault forgotten")
			}
			if _, err := OpenSpool(file, strings.Repeat("1", 32), 4096); !errors.Is(err, ErrGap) {
				t.Fatal("unsafe reopen after I/O fault")
			}
		})
	}
}
