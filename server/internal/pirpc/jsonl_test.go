package pirpc

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type shortReader struct{ r io.Reader }

func (r shortReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}
func TestJSONLChunking(t *testing.T) {
	text := `{"type":"a","text":"中文` + "\u2028\u2029" + `"}`
	r := newJSONLReader(shortReader{strings.NewReader(text + "\r\n" + `{"type":"b"}` + "\n")}, 1024)
	for _, want := range []string{text, `{"type":"b"}`} {
		b, err := r.Next()
		if err != nil || string(b) != want {
			t.Fatalf("%q %v", b, err)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
func TestJSONLLargeRecord(t *testing.T) {
	s := `{"type":"a","text":"` + strings.Repeat("x", 96<<10) + `"}`
	b, err := newJSONLReader(strings.NewReader(s+"\n"), 128<<10).Next()
	if err != nil || string(b) != s {
		t.Fatalf("size=%d %v", len(b), err)
	}
}
func TestJSONLExactLimit(t *testing.T) {
	s := `{"type":"a"}` + strings.Repeat(" ", 244) // 12 + 244 = 256 raw bytes
	if len(s) != 256 {
		t.Fatal(len(s))
	}
	for _, suffix := range []string{"\n", "\r\n", ""} {
		r := newJSONLReader(strings.NewReader(s+suffix), 256)
		b, err := r.Next()
		if err != nil || string(b) != s {
			t.Fatalf("suffix=%q size=%d err=%v", suffix, len(b), err)
		}
		if _, err = r.Next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"\n", "\r\n", ""} {
		if _, err := newJSONLReader(strings.NewReader(s+" "+suffix), 256).Next(); !errors.Is(err, ErrRecordTooLarge) {
			t.Fatal(err)
		}
	}
}
func TestJSONLInvalidRecords(t *testing.T) {
	for _, s := range []string{"\n", "log\n", "null\n", "[]\n", "42\n", "{\"x\":\"\xff\"}\n", `{"x":`, "{\"x\":\"\xe4"} {
		t.Run(s, func(t *testing.T) {
			if _, err := newJSONLReader(bytes.NewBufferString(s), 1024).Next(); !errors.Is(err, ErrProtocol) {
				t.Fatal(err)
			}
		})
	}
}

type endlessReader struct{ read int }

func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read += len(p)
	return len(p), nil
}
func TestJSONLStopsAtLimit(t *testing.T) {
	in := &endlessReader{}
	_, err := newJSONLReader(in, 256).Next()
	if !errors.Is(err, ErrRecordTooLarge) || in.read > 256+4096 {
		t.Fatalf("read=%d err=%v", in.read, err)
	}
}
