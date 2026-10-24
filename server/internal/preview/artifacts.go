package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// maxArtifacts bounds how many downloadable files one manifest may declare, so
// a manifest cannot turn the result panel into an arbitrary file browser.
const maxArtifacts = 16

// downloadMIME is the closed allowlist for explicit artifact downloads. A
// declaration may not invent a type: it either matches the entry for its
// extension or it is refused with the whole manifest.
var downloadMIME = map[string]string{
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".gz":   "application/gzip",
	".txt":  "text/plain; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".json": "application/json",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
}

// artifactDecl is one explicitly declared downloadable file. Path is relative
// to the task worktree; Name is the browser-facing file name and Type must stay
// inside downloadMIME.
type artifactDecl struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// Artifact is the browser-facing descriptor. It never carries a filesystem
// path: the browser may only echo back the opaque ID, and the server re-resolves
// the path from the current manifest on every request.
type Artifact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// resolvedArtifact pairs the public descriptor with the worktree-relative path
// the server will open. The path stays server-side.
type resolvedArtifact struct {
	Artifact
	Path string
}

// checkArtifacts validates the declaration list once, at manifest read time.
// Every entry must be a plain relative path to a file whose extension is in the
// download allowlist; names and types may not smuggle separators, control
// characters or a fabricated content type.
func checkArtifacts(decls []artifactDecl) error {
	if len(decls) > maxArtifacts {
		return fmt.Errorf("预览配置最多声明 %d 个可下载文件", maxArtifacts)
	}
	seen := map[string]bool{}
	for _, d := range decls {
		if parts, ok := staticSegments(d.Path, false); !ok || len(parts) == 0 {
			return errors.New("可下载文件路径必须是工作区内的相对文件")
		}
		if seen[d.Path] {
			return errors.New("可下载文件不能重复声明")
		}
		seen[d.Path] = true
		want, ok := downloadMIMEFor(d.Path)
		if !ok {
			return errors.New("仅支持声明受支持的下载文件类型")
		}
		if d.Type != "" && d.Type != want {
			return errors.New("可下载文件类型必须与文件扩展名一致")
		}
		if d.Name != "" && !validArtifactName(d.Name) {
			return errors.New("可下载文件名称无效")
		}
	}
	return nil
}

// validArtifactName accepts one plain file name. Chinese characters, spaces and
// dots in the middle are fine; separators, control characters, surrounding
// whitespace and the dot segments are not.
func validArtifactName(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || hasControl(name) ||
		strings.ContainsAny(name, "/\\") || strings.TrimSpace(name) != name ||
		name == "." || name == ".." || strings.Contains(name, "\x00") {
		return false
	}
	return true
}

func downloadMIMEFor(name string) (string, bool) {
	mime, ok := downloadMIME[strings.ToLower(filepath.Ext(name))]
	return mime, ok
}

// openArtifactFile opens one declared downloadable file. It reuses the same
// fd-relative NOFOLLOW walk and the same segment rules as static assets, but
// resolves the type from the download allowlist instead of the asset list.
func openArtifactFile(tree, relative string) (*os.File, os.FileInfo, string, error) {
	if tree == "" || !filepath.IsAbs(tree) || strings.ContainsAny(tree, "\x00\r\n") {
		return nil, nil, "", errStaticPath
	}
	parts, ok := staticSegments(relative, false)
	if !ok || len(parts) == 0 {
		return nil, nil, "", errStaticPath
	}
	mime, ok := downloadMIMEFor(parts[len(parts)-1])
	if !ok {
		return nil, nil, "", errStaticPath
	}
	f, info, e := openStaticAt(tree, parts[:len(parts)-1], parts[len(parts)-1])
	if e != nil {
		if f != nil {
			f.Close()
		}
		return nil, nil, "", errStaticPath
	}
	if !info.Mode().IsRegular() || info.Size() > maxStaticFileSize {
		f.Close()
		return nil, nil, "", errStaticPath
	}
	return f, info, mime, nil
}

// artifactID is deterministic for one declared path, so Status/Open show a
// stable identifier while a different path yields a different one.
func artifactID(name string) string {
	sum := sha256.Sum256([]byte("artifact\x00" + name))
	return hex.EncodeToString(sum[:])
}

// artifactFileName is the browser-facing name: the explicit name, else the last
// path segment.
func artifactFileName(d artifactDecl) string {
	if d.Name != "" {
		return d.Name
	}
	return path.Base(d.Path)
}

// resolveArtifacts proves every declared file is currently openable and returns
// its size. A single unusable declaration makes the whole list unavailable: the
// panel may not show a download that would fail later, and it never falls back
// to scanning the worktree.
func resolveArtifacts(tree string, decls []artifactDecl) ([]resolvedArtifact, error) {
	out := make([]resolvedArtifact, 0, len(decls))
	for _, d := range decls {
		f, info, mime, e := openArtifactFile(tree, d.Path)
		if e != nil {
			return nil, e
		}
		f.Close()
		out = append(out, resolvedArtifact{
			Artifact: Artifact{ID: artifactID(d.Path), Name: artifactFileName(d), Size: info.Size(), Type: mime},
			Path:     d.Path,
		})
	}
	return out, nil
}

// resolveArtifactIn resolves one download ID inside an already validated
// manifest snapshot. The snapshot is the one the capability's state check used,
// so a later manifest write can neither add nor retarget a declaration behind an
// already-issued capability.
func resolveArtifactIn(tree string, snap manifest, id string) (resolvedArtifact, error) {
	for _, d := range snap.Artifacts {
		if artifactID(d.Path) != id {
			continue
		}
		f, info, mime, e := openArtifactFile(tree, d.Path)
		if e != nil {
			return resolvedArtifact{}, e
		}
		f.Close()
		return resolvedArtifact{
			Artifact: Artifact{ID: id, Name: artifactFileName(d), Size: info.Size(), Type: mime},
			Path:     d.Path,
		}, nil
	}
	return resolvedArtifact{}, errStaticPath
}

// downloadDisposition builds an attachment header with an ASCII fallback plus
// the exact UTF-8 name, so Chinese names and spaces survive without ever being
// interpreted as a path or an inline document.
func downloadDisposition(name string) string {
	fallback := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			fallback = append(fallback, '_')
			continue
		}
		fallback = append(fallback, r)
	}
	safe := strings.TrimSpace(string(fallback))
	if safe == "" || safe == "." || safe == ".." {
		safe = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", safe, escapeRFC5987(name))
}

// escapeRFC5987 percent-encodes every byte outside the RFC 5987 attr-char set.
func escapeRFC5987(name string) string {
	const attr = "!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(attr, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%")
		b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
	}
	return b.String()
}
