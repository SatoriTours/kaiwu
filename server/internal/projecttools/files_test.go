package projecttools

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesConfinedNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	f, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = f.Mkdir("docs"); e != nil {
		t.Fatal(e)
	}
	if e = f.Upload("docs/note.txt", bytes.NewBufferString("hello")); e != nil {
		t.Fatal(e)
	}
	if e = f.Upload("docs/note.txt", bytes.NewBufferString("replace")); e == nil {
		t.Fatal("overwrote")
	}
	b, e := f.Read("docs/note.txt")
	if e != nil || string(b) != "hello" {
		t.Fatal(string(b), e)
	}
	list, e := f.List("docs")
	if e != nil || len(list) != 1 || list[0].Name != "note.txt" {
		t.Fatal(list, e)
	}
	os.Symlink(t.TempDir(), filepath.Join(dir, "escape"))
	for _, p := range []string{"../outside", "/etc/passwd", ".git/config", "docs/../../x", "escape/x"} {
		if e = f.Upload(p, bytes.NewBufferString("bad")); e == nil {
			t.Fatal("accepted", p)
		}
	}
	if e = f.Upload("big", bytes.NewReader(make([]byte, MaxFile+1))); e == nil {
		t.Fatal("unbounded")
	}
	if _, e = os.Stat(filepath.Join(dir, "big")); !os.IsNotExist(e) {
		t.Fatal("partial file retained")
	}
}
func TestAttachmentOwnershipMIMEAndReload(t *testing.T) {
	a := Attachments{Dir: t.TempDir()}
	item, e := a.Save("conv-a", "note.txt", bytes.NewBufferString("hello"))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.Load("conv-b", item.ID); e == nil {
		t.Fatal("cross-conversation read")
	}
	b, meta, e := a.Load("conv-a", item.ID)
	if e != nil || string(b) != "hello" || meta.Name != "note.txt" {
		t.Fatal(meta, e)
	}
	again := Attachments{Dir: a.Dir}
	if _, _, e = again.Load("conv-a", item.ID); e != nil {
		t.Fatal(e)
	}
	html, e := a.Save("conv-a", "fake.png", bytes.NewBufferString("<html><script>bad</script></html>"))
	if e != nil {
		t.Fatal(e)
	}
	if html.MIME == "image/png" {
		t.Fatal("trusted extension")
	}
	if _, e = a.Save("conv-a", "../bad", bytes.NewBufferString("x")); e == nil {
		t.Fatal("unsafe name")
	}
}
func TestAttachmentStorageQuota(t *testing.T) {
	a := Attachments{Dir: t.TempDir()}
	os.MkdirAll(filepath.Join(a.Dir, "existing"), 0700)
	if e := os.WriteFile(filepath.Join(a.Dir, "existing", "meta.json"), []byte(`{"size":268435456}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Save("conv", "file", bytes.NewBufferString("overflow")); e == nil {
		t.Fatal("storage quota ignored")
	}
}
