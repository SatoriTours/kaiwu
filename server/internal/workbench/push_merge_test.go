package workbench

import "testing"

// Liveness is decided by the phase set: a crash after the local merge or after
// the remote confirmation still owns the workspace, so those phases must be
// incomplete (and must not be treated as terminal).
func TestMergePhaseLiveness(t *testing.T) {
	for _, phase := range []string{"queued", "preparing", "committing", "committed", "merging", "merged", "pushing", "remote_done"} {
		if !MergeActive(phase) || !MergeIncomplete(phase) {
			t.Fatalf("%q must still own the operation", phase)
		}
	}
	for _, phase := range []string{"succeeded", "failed", "needs_check", "", "unknown"} {
		if MergeActive(phase) || MergeIncomplete(phase) {
			t.Fatalf("%q must not be active", phase)
		}
	}
}
