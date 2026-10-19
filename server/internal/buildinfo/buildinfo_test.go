package buildinfo

import (
	"os/exec"
	"strings"
	"testing"
)

// setInjected temporarily sets the linker-injected variables.
func setInjected(t *testing.T, rev, sum, at string) {
	t.Helper()
	oldRev, oldSum, oldAt := revision, summary, builtAt
	revision, summary, builtAt = rev, sum, at
	t.Cleanup(func() { revision, summary, builtAt = oldRev, oldSum, oldAt })
}

func TestCurrentIsUnknownWithoutInjectedValues(t *testing.T) {
	setInjected(t, "", "", "")
	got := Current()
	if got.Revision != Unknown || got.ShortRevision != Unknown || got.Summary != "" || got.BuiltAt != "" {
		t.Fatalf("unexpected unknown info: %+v", got)
	}
}

func TestCurrentNeverReadsCheckoutHead(t *testing.T) {
	// The test binary runs inside the repository worktree, so any checkout
	// probing would find a real commit. An un-injected build must still report
	// dev instead of impersonating a released revision.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	setInjected(t, "", "", "")
	got := Current()
	if got.Revision != Unknown {
		t.Fatalf("development build impersonated a revision: %+v", got)
	}
}

func TestCurrentRejectsInvalidRevision(t *testing.T) {
	for _, rev := range []string{
		"",
		"dev",
		"8b54309",
		strings.Repeat("g", 40),
		strings.ToUpper(strings.Repeat("a", 40)),
		strings.Repeat("a", 39),
		strings.Repeat("a", 41),
		"8b54309f03dd75759291be48ff3f3b766ab89b5 ",
	} {
		setInjected(t, rev, "", "")
		if got := Current(); got.Revision != Unknown || got.ShortRevision != Unknown {
			t.Fatalf("revision %q accepted: %+v", rev, got)
		}
	}
}

func TestCurrentNormalizesInjectedValues(t *testing.T) {
	rev := strings.Repeat("a1b2", 10) // 40 characters of lowercase hex
	setInjected(t, rev, "  修复\t部署\n状态  ", "2026-10-19T10:00:00+08:00")
	got := Current()
	if got.Revision != rev || got.ShortRevision != rev[:7] {
		t.Fatalf("revision: %+v", got)
	}
	if got.Summary != "修复 部署 状态" {
		t.Fatalf("summary not collapsed: %q", got.Summary)
	}
	if got.BuiltAt != "2026-10-19T02:00:00Z" {
		t.Fatalf("builtAt not normalized: %q", got.BuiltAt)
	}
}

func TestCurrentRejectsUnparsableBuildTime(t *testing.T) {
	setInjected(t, strings.Repeat("a", 40), "", "yesterday afternoon")
	if got := Current(); got.BuiltAt != "" {
		t.Fatalf("kept invalid build time: %q", got.BuiltAt)
	}
}

func TestCurrentBoundsSummary(t *testing.T) {
	setInjected(t, strings.Repeat("a", 40), strings.Repeat("摘", maxSummaryRunes+50), "")
	got := Current()
	if n := len([]rune(got.Summary)); n != maxSummaryRunes {
		t.Fatalf("summary runes=%d want %d", n, maxSummaryRunes)
	}
}

func TestValidRevision(t *testing.T) {
	if !ValidRevision(strings.Repeat("0123456789abcdef", 3)[:40]) {
		t.Fatal("valid lowercase hex rejected")
	}
	if ValidRevision("") || ValidRevision("dev") || ValidRevision(strings.Repeat("A", 40)) {
		t.Fatal("invalid revision accepted")
	}
}
