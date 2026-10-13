package pirpc

import (
	"bytes"
	"sync"
	"testing"
)

func TestTailBoundedCopies(t *testing.T) {
	b := newTailBuffer(65536)
	data := append(bytes.Repeat([]byte("a"), 65536), bytes.Repeat([]byte("b"), 65536)...)
	for i := 0; i < len(data); i += 512 {
		n, err := b.Write(data[i : i+512])
		if err != nil || n != 512 {
			t.Fatal(err)
		}
	}
	got := b.Bytes()
	if !bytes.Equal(got, bytes.Repeat([]byte("b"), 65536)) {
		t.Fatal("wrong tail")
	}
	got[0] = 'x'
	if b.Bytes()[0] != 'b' {
		t.Fatal("aliased tail")
	}
	_, _ = b.Write([]byte("end"))
	if !bytes.HasSuffix(b.Bytes(), []byte("end")) || len(b.Bytes()) != 65536 {
		t.Fatal("tail update")
	}
}
func TestTailConcurrentAccess(t *testing.T) {
	b := newTailBuffer(256)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_, _ = b.Write([]byte("0123456789"))
				if len(b.Bytes()) > 256 {
					t.Error("unbounded tail")
				}
			}
		}()
	}
	wg.Wait()
}
