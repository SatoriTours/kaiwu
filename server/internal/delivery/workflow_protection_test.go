package delivery

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Same workflow ID + weaker YAML must never reach commit/push as an automatic
// fix. This exercises the real worker, SQLite, worktree and Git object trees.
func TestWorkflowProtectionStopsGateChanges(t *testing.T) {
	for _, kind := range []string{"edited", "deleted", "added", "committed", "mode", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			m := newMachine(t, true)
			m.waitPhase(t, "developing")
			before := headOf(t, m.tree)
			path := filepath.Join(m.tree, ".github", "workflows", "task.yml")
			switch kind {
			case "deleted":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "added":
				writeFile(t, filepath.Join(m.tree, ".github", "workflows", "extra.yml"), "on: push\njobs: {}\n")
			case "mode":
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../../base.txt", path); err != nil {
					t.Fatal(err)
				}
			default:
				writeFile(t, path, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
				if kind == "committed" {
					before = commitAllIn(t, m.tree)
				}
			}
			m.finishRound(happyReport())
			d := workflowDecision(t, m)
			want := "protected_definitions_changed"
			if kind == "symlink" {
				want = "candidate_unavailable" // Existing special-file gate is earlier.
			}
			if d.BlockCode != want {
				t.Fatalf("workflow change was not stopped by the protected definition gate: %s (%s)", d.BlockCode, d.BlockReason)
			}
			if headOf(t, m.tree) != before || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
				t.Fatal("workflow change caused an automatic commit, push or repair")
			}
		})
	}
}

// Replacement refs are local to the shared repository and are not pushed.
// They must not let ls-tree report safe definitions for a weakened object.
func TestWorkflowProtectionIgnoresReplacementObjects(t *testing.T) {
	for _, kind := range []string{"commit", "tree", "subtree", "blob"} {
		t.Run(kind, func(t *testing.T) {
			m := newMachine(t, true)
			d := m.waitPhase(t, "developing")
			safe := d.LocalMainSHA
			writeFile(t, filepath.Join(m.tree, ".github", "workflows", "task.yml"), "name: weakened\n")
			weak := commitAllIn(t, m.tree)
			weakTree := gitIn(t, m.tree, "rev-parse", weak+"^{tree}")
			old, replacement := weak, safe
			switch kind {
			case "tree":
				old, replacement = weakTree, gitIn(t, m.tree, "rev-parse", safe+"^{tree}")
			case "subtree":
				old = gitIn(t, m.tree, "rev-parse", weak+":.github")
				replacement = gitIn(t, m.tree, "rev-parse", safe+":.github")
			case "blob":
				old = gitIn(t, m.tree, "rev-parse", weak+":.github/workflows/task.yml")
				replacement = gitIn(t, m.tree, "rev-parse", safe+":.github/workflows/task.yml")
			}
			gitIn(t, m.tree, "replace", old, replacement)
			// Both the shared main checkout and task worktree see the ref.
			for _, dir := range []string{m.source, m.tree} {
				for _, obj := range []string{weak, weakTree} {
					if code, _ := m.manager.verifyWorkflowTree(d, dir, obj); code != "protected_definitions_changed" {
						t.Fatalf("%s replacement disguised weakened object: code=%s", kind, code)
					}
				}
				if fp, err := workflowFingerprint(t.Context(), dir, safe); err != nil || fp != d.ProtectedFingerprint {
					t.Fatalf("replacement changed the trusted baseline: %s %v", fp, err)
				}
			}
			copy := d
			copy.Phase, copy.CandidateSHA = "pushing_task", weak
			if code, _ := m.manager.gateCode(copy); code != "protected_definitions_changed" {
				t.Fatalf("replacement bypassed exact push SHA gate: %s", code)
			}
			m.finishRound(happyReport())
			blocked := workflowDecision(t, m)
			if blocked.Phase != "blocked" || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
				t.Fatalf("replacement reached automatic commit/push/repair: %s", blocked.Phase)
			}
		})
	}
}

func TestWorkflowBaselineIsDurableBeforeDevelopment(t *testing.T) {
	m := newMachine(t, true)
	d := m.waitPhase(t, "developing")
	if d.ProtectedFingerprint == "" {
		t.Fatal("workflow baseline was not persisted before dispatch/development")
	}
	// A fresh store read is the same serialized row used after restart.
	reloaded, err := m.store.Delivery(d.ID)
	if err != nil || reloaded.ProtectedFingerprint != d.ProtectedFingerprint {
		t.Fatal("workflow baseline did not survive persistence")
	}
}

func workflowDecision(t *testing.T, m *machine) workbench.Delivery {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m.clock.advance(time.Millisecond)
		d, err := m.store.Delivery(m.deliveryID)
		if err != nil {
			t.Fatal(err)
		}
		switch d.Phase {
		case "blocked", "needs_check", "awaiting_acceptance", "succeeded":
			return d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker did not reach a workflow decision")
	return workbench.Delivery{}
}

func TestWorkflowChangeDuringCheckCannotTriggerAutomaticRepair(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
		p.Checks = []workbench.DeliveryCheck{{
			ID: "rewrite-workflow", Dir: ".", TimeoutSeconds: 10,
			Argv: []string{"sh", "-c", "printf 'name: weakened\\n' > .github/workflows/task.yml"},
		}}
	})
	m.waitPhase(t, "developing")
	m.finishRound(happyReport())
	d := workflowDecision(t, m)
	if d.BlockCode != "protected_definitions_changed" || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
		t.Fatalf("check-time rewrite was accepted/repaired: %s", d.BlockCode)
	}
}

func TestWorkflowGateRechecksExactCommitBeforePushAndRecovery(t *testing.T) {
	m := newMachine(t, true)
	d := m.waitPhase(t, "developing")
	writeFile(t, filepath.Join(m.tree, ".github", "workflows", "task.yml"), "name: weakened\n")
	sha := commitAllIn(t, m.tree)
	for _, phase := range []string{"committing", "pushing_task", "task_ci", "integrating", "pushing_main", "main_ci"} {
		copy := d
		copy.Phase, copy.CandidateSHA, copy.LocalMainSHA = phase, sha, sha
		if code, _ := m.manager.gateCode(copy); code != "protected_definitions_changed" {
			t.Fatalf("%s trusted changed commit: %s", phase, code)
		}
	}
	copy := d
	copy.ProtectedFingerprint = ""
	if code, _ := m.manager.gateCode(copy); code != "protected_definitions_missing" {
		t.Fatalf("legacy active cycle silently adopted new definitions: %s", code)
	}
}
