// Package buildinfo reports the revision embedded at build time and reads the
// bounded, read-only deployment status written by the local deployment scripts.
//
// The running revision always comes from the compiled binary. A development
// build without injected values reports the literal "dev" and never inspects
// the checkout's Git HEAD, so an uncommitted or dirty tree cannot impersonate
// a released revision.
package buildinfo

import (
	"strings"
	"time"
	"unicode"
)

// Unknown is the revision reported when no valid build-time revision exists.
const Unknown = "dev"

// revision, summary and builtAt are injected by the local build script with
// -ldflags -X github.com/SatoriTours/kaiwu/server/internal/buildinfo.<name>.
var (
	revision = ""
	summary  = ""
	builtAt  = ""
)

const (
	shortRevisionLen = 7
	maxSummaryRunes  = 200
)

// Info is the running binary's own version metadata.
type Info struct {
	Revision      string `json:"revision"`
	ShortRevision string `json:"shortRevision"`
	Summary       string `json:"summary"`
	BuiltAt       string `json:"builtAt"`
}

// Current normalizes the injected values. It never reads Git or any file.
func Current() Info {
	return normalize(Info{Revision: revision, Summary: summary, BuiltAt: builtAt})
}

func normalize(in Info) Info {
	out := Info{Revision: Unknown, ShortRevision: Unknown}
	if ValidRevision(in.Revision) {
		out.Revision = in.Revision
		out.ShortRevision = in.Revision[:shortRevisionLen]
	}
	out.Summary = cleanSummary(in.Summary)
	out.BuiltAt = cleanTime(in.BuiltAt)
	return out
}

// ValidRevision reports whether s is a full lowercase 40-character Git object
// id. Anything else is an unknown development build.
func ValidRevision(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// cleanSummary collapses whitespace and control characters and bounds the
// length so an injected commit subject stays a single short line.
func cleanSummary(s string) string {
	var b strings.Builder
	pendingSpace := false
	runes := 0
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			if b.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if runes >= maxSummaryRunes {
			break
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
			runes++
		}
		b.WriteRune(r)
		runes++
	}
	return b.String()
}

// cleanTime normalizes an RFC3339 timestamp to UTC. An unparsable value stays
// empty instead of being echoed back.
func cleanTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
