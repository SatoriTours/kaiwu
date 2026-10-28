package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/projecttools"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"testing"
)

func TestPromptContainsRealImageAndFilePath(t *testing.T) {
	a := projecttools.Attachments{Dir: t.TempDir()}
	image, e := a.Save("conv", "shot.png", bytes.NewReader([]byte{137, 80, 78, 71, 13, 10, 26, 10, 1}))
	if e != nil {
		t.Fatal(e)
	}
	doc, e := a.Save("conv", "spec.txt", bytes.NewBufferString("requirements"))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := attachmentPrompt(a, "conv", "hello", []workbench.Attachment{image, doc})
	if e != nil {
		t.Fatal(e)
	}
	var p struct {
		Message string
		Images  []struct{ Type, MimeType, Data string }
	}
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Images) != 1 || p.Images[0].Type != "image" || p.Images[0].MimeType != "image/png" {
		t.Fatal(string(raw))
	}
	data, _ := base64.StdEncoding.DecodeString(p.Images[0].Data)
	if !bytes.Equal(data, []byte{137, 80, 78, 71, 13, 10, 26, 10, 1}) {
		t.Fatal("not image bytes")
	}
	if !bytes.Contains([]byte(p.Message), []byte(a.Path(doc.ID))) {
		t.Fatal(p.Message)
	}
	if _, e = attachmentPrompt(a, "other", "", []workbench.Attachment{doc}); e == nil {
		t.Fatal("wrong owner")
	}
}
