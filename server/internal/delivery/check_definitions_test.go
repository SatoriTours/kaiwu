package delivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func declaredCheckPolicy(t *testing.T, p *workbench.ProjectDeliveryPolicy) {
	t.Helper()
	if err := json.Unmarshal([]byte(`{"checks":[{"id":"script","argv":["sh","scripts/check.sh"],"dir":".","timeoutSeconds":30,"definitionPaths":["scripts/check.sh"]}]}`), p); err != nil {
		t.Fatal(err)
	}
}

// Removing definition enforcement must let these weakened scripts progress.
func TestCheckDefinitionsStopScriptChanges(t *testing.T) {
	for _, kind := range []string{"edited", "committed", "mode", "deleted", "symlink", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) { declaredCheckPolicy(t, p) })
			d := m.waitPhase(t, "developing")
			path := filepath.Join(m.tree, "scripts", "check.sh")
			before := headOf(t, m.tree)
			switch kind {
			case "mode":
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../base.txt", path); err != nil {
					t.Fatal(err)
				}
			default:
				writeFile(t, path, "exit 0 # weakened\n")
				if kind == "committed" || kind == "replacement" {
					before = commitAllIn(t, m.tree)
					if kind == "replacement" {
						gitIn(t, m.tree, "replace", gitIn(t, m.tree, "rev-parse", before+":scripts"), gitIn(t, m.tree, "rev-parse", d.LocalMainSHA+":scripts"))
					}
				}
			}
			m.finishRound(happyReport())
			blocked := workflowDecision(t, m)
			want := "protected_definitions_changed"
			if kind == "deleted" {
				want = "protected_definitions_unavailable"
			}
			if kind == "symlink" {
				want = "candidate_unavailable"
			}
			if blocked.Phase != "blocked" || blocked.BlockCode != want {
				t.Fatalf("weakened check not stopped: phase=%s code=%s", blocked.Phase, blocked.BlockCode)
			}
			if headOf(t, m.tree) != before || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
				t.Fatal("weakened check caused commit, push or repair")
			}
			reloaded, err := m.store.Delivery(d.ID)
			if err != nil || reloaded.ProtectedFingerprint != d.ProtectedFingerprint {
				t.Fatal("definition baseline was rewritten")
			}
		})
	}
}
func TestCheckDefinitionsChangedDuringCheckNeverRepair(t *testing.T) {
	for _, kind := range []string{"edit", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
				declaredCheckPolicy(t, p)
				command := "printf 'exit 0\\n' > scripts/check.sh"
				if kind == "symlink" {
					command = "rm scripts/check.sh; ln -s ../base.txt scripts/check.sh"
				}
				p.Checks[0].Argv = []string{"sh", "-c", command}
			})
			m.waitPhase(t, "developing")
			m.finishRound(happyReport())
			d := workflowDecision(t, m)
			want := "protected_definitions_changed"
			if kind == "symlink" {
				want = "protected_definitions_unavailable"
			}
			if d.BlockCode != want || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
				t.Fatalf("check-time definition change escaped guard: phase=%s code=%s pushes=%d repairs=%d", d.Phase, d.BlockCode, m.remote.pushCount(), m.agent.repairsCount())
			}
		})
	}
}

func TestCheckDefinitionsUnavailableBeforeDispatch(t *testing.T) {
	for _, path := range []string{"missing.sh", "scripts"} {
		t.Run(path, func(t *testing.T) {
			m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) {
				declaredCheckPolicy(t, p)
				p.Checks[0].DefinitionPaths = []string{path}
			})
			d := workflowDecision(t, m)
			if d.BlockCode != "protected_definitions_unavailable" || len(m.agent.sent()) != 0 || m.remote.pushCount() != 0 || m.agent.repairsCount() != 0 {
				t.Fatalf("missing/nonfile definition was dispatched: %s sends=%d", d.BlockCode, len(m.agent.sent()))
			}
		})
	}
}

func TestCheckDefinitionsGateExactCandidateAndMain(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) { declaredCheckPolicy(t, p) })
	d := m.waitPhase(t, "developing")
	writeFile(t, filepath.Join(m.tree, "scripts", "check.sh"), "exit 0 # weakened\\n")
	weak := commitAllIn(t, m.tree)
	for _, phase := range []string{"pushing_task", "task_ci", "integrating", "integration_checking", "pushing_main", "main_ci"} {
		copy := d
		copy.Phase, copy.CandidateSHA = phase, weak
		if code, _ := m.manager.gateCode(copy); code != "protected_definitions_changed" {
			t.Fatalf("%s candidate bypass: %s", phase, code)
		}
	}
	copy := d
	copy.CandidateSHA, copy.LocalMainSHA = d.LocalMainSHA, weak
	for _, phase := range []string{"integration_checking", "pushing_main", "main_ci"} {
		copy.Phase = phase
		if code, _ := m.manager.gateCode(copy); code != "protected_definitions_changed" {
			t.Fatalf("%s main bypass: %s", phase, code)
		}
	}
}

func TestCheckDefinitionsPermitNormalFeature(t *testing.T) {
	m := newMachineWithPolicy(t, true, func(p *workbench.ProjectDeliveryPolicy) { declaredCheckPolicy(t, p) })
	d := m.waitPhase(t, "developing")
	if d.ProtectedFingerprint == "" {
		t.Fatal("missing durable definition baseline")
	}
	m.finishRound(happyReport())
	if got := workflowDecision(t, m); got.Phase != "awaiting_acceptance" {
		t.Fatalf("normal development blocked: %s %s", got.Phase, got.BlockCode)
	}
}
