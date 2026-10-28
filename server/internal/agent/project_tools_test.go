package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"path/filepath"
	"testing"
)

func TestManualToolsRefuseBusyProjectAndRetainReservation(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "state.db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Command(workbench.Command{Type: "add_project", OperationID: "tools-project", Revision: s.Snapshot().Revision, Name: "tools", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	m := New(context.Background(), s, Config{DataDir: t.TempDir()})
	defer m.Close()
	s.Update(func(st *workbench.State) error {
		c := st.Conversations[p.ConversationID]
		c.RunState = "running"
		st.Conversations[c.ID] = c
		return nil
	})
	if _, e = m.AcquireToolLease(context.Background(), p.ID, ""); e == nil {
		t.Fatal("busy project acquired")
	}
}
func TestManualProjectReservationBlocksNewSend(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "state.db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Command(workbench.Command{Type: "add_project", OperationID: "tools-lock-project", Revision: s.Snapshot().Revision, Name: "tools", Path: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	m := New(context.Background(), s, Config{DataDir: t.TempDir()})
	defer m.Close()
	clear, e := m.reserveProjectTools(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	if !m.ToolWriteBlocked(p.ID, "") {
		t.Fatal("reservation not visible to admission")
	}
	clear()
	if m.ToolWriteBlocked(p.ID, "") {
		t.Fatal("reservation retained after safe release")
	}
}
