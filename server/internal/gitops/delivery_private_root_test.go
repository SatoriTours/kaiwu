package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryCandidateProtectsCustomServiceDataRoot(t *testing.T) {
	p, task, x := candidateFixture(t)
	dataRoot := filepath.Join(p.Path, "custom-runtime")
	if err := os.MkdirAll(filepath.Join(x.Workspace, "custom-runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.Workspace, "custom-runtime", "state.json"), []byte("synthetic private runtime data"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, x.Workspace, "add", ".")
	git(t, x.Workspace, "commit", "-m", "runtime data")
	if _, err := CaptureCandidate(context.Background(), p, task, x, dataRoot); err == nil {
		t.Fatal("clean committed custom service data was not protected")
	}
}
