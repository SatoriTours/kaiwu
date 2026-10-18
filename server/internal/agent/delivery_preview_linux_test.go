//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/preview"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestDeliveryStopsRealPreviewBeforeTakingIntegrationLease(t *testing.T) {
	m, task, x, d, _ := deliveryLeaseFixture(t)
	if err := m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.RunState = "idle"
		st.Conversations[c.ID] = c
		workbench.UpdateRound(st, c.ID, d.RoundID, func(r *workbench.Round) { r.Phase = "finished"; r.FinishedAt = time.Now().UTC().Format(time.RFC3339) })
		exec := st.Executions[x.ID]
		exec.Phase = "awaiting_review"
		exec.FinishedAt = "done"
		st.Executions[x.ID] = exec
		task.Status = "needs_confirmation"
		st.Tasks[task.ID] = task
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	manifest, _ := json.Marshal(map[string]any{"version": 1, "command": []string{exe, "-test.run=TestTaskPreviewHTTPHelper", "--", "task-preview", "{port}"}, "path": "/"})
	if err := os.WriteFile(filepath.Join(x.Workspace, "kaiwu-preview.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	pm := preview.New(context.Background(), m.store, preview.Config{DataDir: m.cfg.DataDir, Reserve: m.AcquirePreviewLease})
	defer pm.Close()
	m.SetPreviewStopper(pm.StopTask)
	req := preview.Request{ProjectID: task.ProjectID, TaskID: task.ID, RoundID: d.RoundID, OperationID: "delivery-preview"}
	shown, err := pm.Status(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ManifestID = shown.ManifestID
	if v, e := pm.Start(req); e != nil || v.State != "running" {
		t.Fatal(v, e)
	}
	lease, err := m.AcquireDeliveryLease(context.Background(), d.ID, "project_task")
	if err != nil {
		t.Fatalf("real effect preview blocked delivery handoff: %v", err)
	}
	defer lease.Release(true)
	if v, e := pm.Status(req); e != nil || v.State == "running" {
		t.Fatal("delivery did not reliably stop preview", v, e)
	}
}
