package githubauth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenSSH -i stats the literal filename before expanding IdentityFile tokens.
// Escaping % as %% does not preserve the filename. Reject this unsupported
// input explicitly rather than returning a credential that cannot authenticate.
func TestSSHRejectsTokenizedKeyPaths(t *testing.T) {
	for _, name := range []string{"key%d", "key%%d", "key%h"} {
		t.Run(name, func(t *testing.T) {
			key := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(key, []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			r, _ := New(Config{Lookup: envLookup(map[string]string{envSSHKey: key})})
			cred, err := r.Git(context.Background(), RefSSH, "ssh://git@github.com/owner/repo.git")
			if err == nil {
				cred.Close()
				t.Fatal("unusable percent-token key path accepted")
			}
			if !strings.Contains(err.Error(), "%") {
				t.Fatal("unsupported key path lacks an actionable reason")
			}
		})
	}
}
