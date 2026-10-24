package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
)

// maxStaticFileSize bounds one served static asset. Files are streamed from the
// worktree, so the limit is enforced from fstat before any byte is read.
const maxStaticFileSize int64 = 32 << 20

// errStaticPath covers every refusal reason. Callers present one generic
// message so a probe cannot learn whether a hidden/private path exists.
var errStaticPath = errors.New("静态资源不可访问")

// staticMIME is a closed allowlist of browser assets. Anything else (sources,
// keys, configuration, data files) is refused rather than guessed.
var staticMIME = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".bmp":   "image/bmp",
	".ico":   "image/x-icon",
	".svg":   "image/svg+xml",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".wasm":  "application/wasm",
}

// openStaticFile opens one static asset for a later gateway response. tree is
// the server-known task worktree; root is a manifest-relative directory and
// relative a request-relative asset path. The caller closes the file. No
// directory listing, no read into memory and no symlink traversal happens here:
// the platform opener resolves every path segment itself after validation.
func openStaticFile(tree, root, relative string) (*os.File, os.FileInfo, string, error) {
	if tree == "" || !filepath.IsAbs(tree) || strings.ContainsAny(tree, "\x00\r\n") {
		return nil, nil, "", errStaticPath
	}
	dirs, ok := staticSegments(root, true)
	if !ok {
		return nil, nil, "", errStaticPath
	}
	parts, ok := staticSegments(relative, false)
	if !ok || len(parts) == 0 {
		return nil, nil, "", errStaticPath
	}
	mime, ok := staticMIMEFor(parts[len(parts)-1])
	if !ok {
		return nil, nil, "", errStaticPath
	}
	f, info, e := openStaticAt(tree, append(dirs, parts[:len(parts)-1]...), parts[len(parts)-1])
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

// staticSegments validates a manifest- or request-relative path without
// touching the filesystem. allowRoot permits the single "." root form.
func staticSegments(name string, allowRoot bool) ([]string, bool) {
	if allowRoot && name == "." {
		return nil, true
	}
	if name == "" || len(name) > 2048 || !utf8.ValidString(name) || hasControl(name) ||
		strings.ContainsAny(name, "\\%") || strings.HasPrefix(name, "/") || !filepath.IsLocal(name) {
		return nil, false
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		// Hidden entries, empty or dot segments and padded names are refused so
		// one spelling can never reach a second path.
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") ||
			len(part) > 255 || strings.TrimSpace(part) != part || !utf8.ValidString(part) || hasControl(part) {
			return nil, false
		}
	}
	if gitops.PrivatePath(strings.Join(parts, "/")) {
		return nil, false
	}
	return parts, true
}

// hasControl refuses every C0 control, DEL, C1 control and Unicode format
// character, so no request can smuggle separators, terminal escapes or
// bidirectional overrides into a served path.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

func staticMIMEFor(name string) (string, bool) {
	mime, ok := staticMIME[strings.ToLower(filepath.Ext(name))]
	return mime, ok
}
