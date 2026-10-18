//go:build linux

package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type refusedDispatch struct{ fakeAgentPort }

func (p *refusedDispatch) Send(string, string) error { return errors.New("session capacity reached") }

func TestDeliveryRefusedDispatchCannotBecomeGhostDevelopment(t *testing.T) {
	w := buildRecoveryWorld(t, t.TempDir())
	defer w.store.Close()
	m := &Manager{ctx: context.Background(), store: w.store, cfg: Config{Agent: &refusedDispatch{}, Clock: newFakeClock()}, dispatched: map[string]bool{}}
	d, err := w.store.Delivery(w.delivery)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.stageQueued(d)
	if next.Phase != "blocked" || next.BlockCode != "dispatch_failed" {
		t.Fatalf("refused dispatch was not durably stopped: %s %s", next.Phase, next.BlockCode)
	}
	stored, err := w.store.Delivery(w.delivery)
	if err != nil || stored.Phase != "blocked" {
		t.Fatalf("CAS lost failure: %+v %v", stored, err)
	}
	if m.dispatchedBefore(d.ID) {
		t.Fatal("refused dispatch recorded as a successful send")
	}
}

func TestDeliveryDispatchPrecedesWorkspacePreparation(t *testing.T) {
	w := buildRecoveryWorld(t, t.TempDir())
	defer w.store.Close()
	// A real newly-admitted task has no workspace until Agent.Send prepares it.
	if err := w.store.Update(func(st *workbench.State) error {
		task := st.Tasks[w.task]
		x := st.Executions[task.ExecutionID]
		x.Workspace = ""
		x.BaseSHA = ""
		x.Branch = ""
		st.Executions[x.ID] = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	port := &fakeAgentPort{}
	m := &Manager{ctx: context.Background(), store: w.store, cfg: Config{Agent: port, Clock: newFakeClock()}, dispatched: map[string]bool{}}
	d, err := w.store.Delivery(w.delivery)
	if err != nil {
		t.Fatal(err)
	}
	next, cont := m.stageQueued(d)
	if !cont || next.Phase != "developing" {
		t.Fatalf("new task blocked before Agent could prepare its workspace: phase=%s code=%s", next.Phase, next.BlockCode)
	}
	if len(port.sent()) != 1 {
		t.Fatal("original task round was not dispatched exactly once")
	}
	if next.BaseMainSHA != w.base {
		t.Fatalf("new task did not freeze the main baseline: %q", next.BaseMainSHA)
	}
}
