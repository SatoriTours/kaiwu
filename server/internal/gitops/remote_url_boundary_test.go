package gitops

import "testing"

// The last remote entry gate must reject transports capable of invoking local
// helpers or sending credentials to an unapproved host, even for manual targets.
func TestRemoteURLBoundaryRejectsUnapprovedTransports(t *testing.T) {
	for _, raw := range []string{
		"ext::sh -c true", "file:///tmp/owner/repo.git",
		"https://evil.invalid/owner/repo.git", "http://github.com/owner/repo.git",
		"https://github.com/owner/repo.git?redirect=evil",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parsed(raw); err == nil {
				t.Fatal("unapproved remote transport accepted")
			}
		})
	}
	for _, raw := range []string{"https://github.com/owner/repo.git", "git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo.git"} {
		if _, err := parsed(raw); err != nil {
			t.Fatalf("approved remote rejected: %s: %v", raw, err)
		}
	}
}
