package projecttools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Attachment = workbench.Attachment
type attachmentRecord struct {
	Attachment
	ConversationID string `json:"conversationId"`
}
type Attachments struct{ Dir string }

var attachmentWrites sync.Mutex

func identifier(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 16 }
func SafeName(name string) bool {
	return name != "" && len(name) <= 240 && name != "." && name != ".." && !strings.ContainsAny(name, "/\\:\r\n\x00")
}
func (a Attachments) Save(conv, name string, r io.Reader) (Attachment, error) {
	attachmentWrites.Lock()
	defer attachmentWrites.Unlock()
	if conv == "" || !SafeName(name) {
		return Attachment{}, errors.New("附件名称无效")
	}
	b, e := io.ReadAll(io.LimitReader(r, MaxFile+1))
	if e != nil {
		return Attachment{}, e
	}
	if len(b) > MaxFile {
		return Attachment{}, errors.New("附件超过 8 MiB")
	}
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		return Attachment{}, e
	}
	meta := Attachment{ID: hex.EncodeToString(id), Name: name, MIME: http.DetectContentType(b), Size: len(b)}
	if e = os.MkdirAll(a.Dir, 0700); e != nil {
		return Attachment{}, e
	}

	dir, e := os.Open(a.Dir)
	if e != nil {
		return Attachment{}, e
	}
	entries, e := dir.ReadDir(1025)
	dir.Close()
	if e != nil && e != io.EOF {
		return Attachment{}, e
	}
	if len(entries) >= 1024 {
		return Attachment{}, errors.New("附件存储已达 1024 项上限；旧附件保留，未自动清理")
	}
	total := len(b)
	for _, entry := range entries {
		// Preserve crash residue without letting a partial record poison every
		// subsequent upload. It still consumes both entry and byte quota.
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "upload-") {
			partial, e := os.Lstat(filepath.Join(a.Dir, entry.Name(), "data"))
			if e == nil {
				if !partial.Mode().IsRegular() {
					return Attachment{}, errors.New("上传暂存记录无效")
				}
				total += int(partial.Size())
			} else if !os.IsNotExist(e) {
				return Attachment{}, e
			}
			continue
		}
		data, e := os.ReadFile(filepath.Join(a.Dir, entry.Name(), "meta.json"))
		if e != nil {
			return Attachment{}, errors.New("附件存储存在未完成记录，请人工检查；未覆盖或清理")
		}
		var meta Attachment
		if json.Unmarshal(data, &meta) != nil || meta.Size < 0 {
			return Attachment{}, errors.New("附件存储记录无效")
		}
		total += meta.Size
	}
	if total > 256<<20 {
		return Attachment{}, errors.New("附件存储已达 256 MiB 上限；旧附件保留，未自动清理")
	}
	// One directory per upload gives atomic visibility; incomplete uploads are not addressable.
	temp, e := os.MkdirTemp(a.Dir, "upload-")
	if e != nil {
		return Attachment{}, e
	}
	defer os.RemoveAll(temp)
	data, _ := json.Marshal(attachmentRecord{meta, conv})
	if e = os.WriteFile(filepath.Join(temp, "data"), b, 0600); e != nil {
		return Attachment{}, e
	}
	if e = os.WriteFile(filepath.Join(temp, "meta.json"), data, 0600); e != nil {
		return Attachment{}, e
	}
	if e = os.Rename(temp, filepath.Join(a.Dir, meta.ID)); e != nil {
		return Attachment{}, e
	}
	return meta, nil
}
func (a Attachments) Load(conv, id string) ([]byte, Attachment, error) {
	if !identifier(id) {
		return nil, Attachment{}, errors.New("附件不存在")
	}
	dir := filepath.Join(a.Dir, id)
	metaBytes, e := os.ReadFile(filepath.Join(dir, "meta.json"))
	if e != nil {
		return nil, Attachment{}, errors.New("附件不存在")
	}
	var record attachmentRecord
	if json.Unmarshal(metaBytes, &record) != nil || record.ID != id || record.ConversationID != conv {
		return nil, Attachment{}, errors.New("附件不属于当前会话")
	}
	f, e := os.Open(filepath.Join(dir, "data"))
	if e != nil {
		return nil, Attachment{}, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if e != nil || len(b) > MaxFile || len(b) != record.Size {
		return nil, Attachment{}, errors.New("附件内容不可用")
	}
	return b, record.Attachment, nil
}
func (a Attachments) Path(id string) string { return filepath.Join(a.Dir, id, "data") }
func ImageMIME(m string) bool {
	return m == "image/png" || m == "image/jpeg" || m == "image/gif" || m == "image/webp"
}
