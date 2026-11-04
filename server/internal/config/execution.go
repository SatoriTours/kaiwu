package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type Execution struct {
	Configured     bool                `json:"-"`
	Version        string              `json:"-"`
	SchemaVersion  int                 `json:"schemaVersion"`
	MaxResidentCLI int                 `json:"maxResidentCLI"`
	GlobalBudget   executionenv.Budget `json:"globalBudget"`
	DefaultBudget  executionenv.Budget `json:"defaultBudget"`
	Toolchains     []string            `json:"toolchains"`
}

// LoadExecution reads requested policy only, never capability assertions.
// Missing configuration leaves managed execution unconfigured without creating
// directories or breaking legacy startup. Callers must still verify real OS,
// quota and network observations; this file cannot enable managed projects.
func LoadExecution(root string) (Execution, error) {
	bad := func() (Execution, error) { return Execution{}, executionenv.ErrInvalid }
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return bad()
	}
	r, err := openExecutionRoot(root)
	if os.IsNotExist(err) {
		return Execution{}, nil
	}
	if err != nil {
		return bad()
	}
	defer r.Close()
	f, err := openExecutionFile(r)
	if os.IsNotExist(err) {
		return Execution{}, nil
	}
	if err != nil {
		return bad()
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || actual.Size() > 65536 {
		return bad()
	}
	body, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(body) > 65536 {
		return bad()
	}
	if !uniqueExecutionKeys(body) {
		return bad()
	}
	var c Execution
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return bad()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return bad()
	}
	if c.SchemaVersion != 1 || c.MaxResidentCLI < 1 || c.MaxResidentCLI > 8 || c.GlobalBudget.Validate() != nil || c.DefaultBudget.Validate() != nil {
		return bad()
	}
	g, d := c.GlobalBudget, c.DefaultBudget
	if d.CPUQuotaUSec > g.CPUQuotaUSec || d.MemoryBytes > g.MemoryBytes || d.PidsMax > g.PidsMax || d.DiskBytes > g.DiskBytes || len(c.Toolchains) == 0 || len(c.Toolchains) > 64 {
		return bad()
	}
	seen := map[string]bool{}
	for _, p := range c.Toolchains {
		if !path.IsAbs(p) || path.Clean(p) != p || strings.ContainsFunc(p, unicode.IsControl) || seen[p] {
			return bad()
		}
		switch p {
		case "/", "/home", "/root", "/proc", "/sys", "/dev", "/run", "/run/user":
			return bad()
		}
		if strings.HasPrefix(p, "/home/") && strings.Count(p, "/") == 2 {
			return bad()
		}
		seen[p] = true
	}
	sum := sha256.Sum256(body)
	c.Version = hex.EncodeToString(sum[:])
	c.Configured = true
	return c, nil
}

// encoding/json matches struct keys case-insensitively. Refuse both exact and
// case-folded duplicates instead of letting an ambiguous later value win.
// This fixed schema has ASCII keys. Reject non-ASCII keys before decoding so
// Unicode simple-fold aliases (long s, Kelvin sign) cannot override a field.
func uniqueExecutionKeys(body []byte) bool {
	d := json.NewDecoder(bytes.NewReader(body))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 16 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok {
					return false
				}
				for i := 0; i < len(name); i++ {
					if name[i] >= 128 {
						return false
					}
				}
				name = strings.ToLower(name)
				if seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
