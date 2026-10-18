//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// deliveryLeaseFixture prepares a task worktree, enables delivery policy and
// records a persistent delivery cycle, so the lease can be purchased by ID.
func deliveryLeaseFixture(t *testing.T) (*Manager, workbench.Task, workbench.Execution, workbench.Delivery, string) {
	t.Helper()
	m, _, _, source := workspaceFixture(t)
	s := m.store
	if e := s.Update(func(st *workbench.State) error {
		st.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(source, "base.txt"), []byte("base\n"), 0600)
	workspaceGit(t, source, "add", ".")
	workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
	r, e := s.Command(workbench.Command{OperationID: "create-task", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: "p", Title: "delivery", Request: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	task := s.Snapshot().Tasks[r.CreatedID]
	x, e := m.prepareWorkspace(context.Background(), task, workbench.Execution{ID: "seed", TaskID: task.ID, ConversationID: task.ConversationID})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Update(func(st *workbench.State) error {
		ex := x
		ex.Phase = "ready"
		st.Executions[ex.ID] = ex
		st.Tasks[task.ID] = workbench.Task{ID: task.ID, ProjectID: task.ProjectID, TopicID: task.TopicID, Title: task.Title, Request: task.Request, Status: "idle", ConversationID: task.ConversationID, ExecutionID: x.ID}
		c := st.Conversations[task.ConversationID]
		c.RunState = "idle"
		st.Conversations[c.ID] = c
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	p := workbench.DefaultDeliveryPolicy("p")
	p.Enabled = true
	p.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	p.Checks = []workbench.DeliveryCheck{{ID: "test", Argv: []string{"echo", "ok"}, Dir: ".", TimeoutSeconds: 300}}
	p.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	p.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2}}
	p.GitCredentialRef = "server_github_https"
	p.APICredentialRef = "server_github_api"
	p.AuthorizationDigest = "explicit-scope"
	p.Actor = "local-login"
	p.AuthorizedAt = time.Now().UTC()
	if _, e = s.SaveDeliveryPolicy("enable-delivery", 0, p); e != nil {
		t.Fatal(e)
	}
	start, e := s.Command(workbench.Command{OperationID: "start-delivery", Revision: s.Snapshot().Revision, Type: "start_task", ProjectID: "p", TaskID: task.ID})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.Delivery(start.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	fresh := s.Snapshot()
	return m, fresh.Tasks[task.ID], fresh.Executions[fresh.Tasks[task.ID].ExecutionID], d, start.CreatedID
}

func waitIdle(t *testing.T, m *Manager, conversation string) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		c := m.store.Snapshot().Conversations[conversation]
		if c.RunState == "idle" {
			return
		}
		if c.RunState == "failed" || c.RunState == "interrupted" {
			t.Fatal(c.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session did not settle")
}

func releaseLease(t *testing.T, lease DeliveryLease, safe bool) {
	t.Helper()
	if e := lease.Release(safe); e != nil {
		t.Fatal(e)
	}
}

func TestDeliveryLeaseHandoff(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown delivery is rejected", func(t *testing.T) {
		m, _, _, _, _ := deliveryLeaseFixture(t)
		if _, e := m.AcquireDeliveryLease(ctx, strings.Repeat("b", 32), "task"); e == nil {
			t.Fatal("unknown delivery accepted")
		}
	})

	t.Run("task lease granted and reserved until release", func(t *testing.T) {
		m, _, x, d, _ := deliveryLeaseFixture(t)
		lease, e := m.AcquireDeliveryLease(ctx, d.ID, "task")
		if e != nil {
			t.Fatal(e)
		}
		if lease.File() == nil {
			t.Fatal("lease lacks inherited lock file")
		}
		if again, e := m.AcquireDeliveryLease(ctx, d.ID, "task"); e == nil {
			_ = again.Release(true)
			t.Fatal("double reservation accepted")
		}
		releaseLease(t, lease, true)
		release, e := m.AcquireDeliveryLease(ctx, d.ID, "task")
		if e != nil {
			t.Fatal("lease not retryable after release", e)
		}
		releaseLease(t, release, true)
		if head, _ := gitops.Head(ctx, x.Workspace, "HEAD"); head == "" {
			t.Fatal("workspace lost")
		}
	})

	t.Run("invalid scope is rejected", func(t *testing.T) {
		m, _, _, d, _ := deliveryLeaseFixture(t)
		if _, e := m.AcquireDeliveryLease(ctx, d.ID, "auto"); e == nil {
			t.Fatal("invalid scope accepted")
		}
	})

	t.Run("project_task holds project and task locks", func(t *testing.T) {
		m, _, x, d, _ := deliveryLeaseFixture(t)
		lease, e := m.AcquireDeliveryLease(ctx, d.ID, "project_task")
		if e != nil {
			t.Fatal(e)
		}
		common, e := gitops.CommonDir(ctx, x.Workspace)
		if e != nil {
			t.Fatal(e)
		}
		if p, e := acquireProjectMergeLease(common); e == nil {
			_ = p.release(true)
			t.Fatal("project lock not held")
		}
		if q, e := acquireExecutionLease(x.Workspace); e == nil {
			_ = q.release(true)
			t.Fatal("task lock not held")
		}
		releaseLease(t, lease, true)
		if p, e := acquireProjectMergeLease(common); e != nil {
			t.Fatal("project lock stayed closed", e)
		} else {
			_ = p.release(true)
		}
		if q, e := acquireExecutionLease(x.Workspace); e != nil {
			t.Fatal("task lock stayed closed", e)
		} else {
			_ = q.release(true)
		}
	})

	t.Run("busy writer rejected without parking reservation", func(t *testing.T) {
		m, task, _, d, _ := deliveryLeaseFixture(t)
		m.instances[task.ConversationID] = &instance{busy: true}
		if _, e := m.AcquireDeliveryLease(ctx, d.ID, "task"); e == nil {
			t.Fatal("busy writer accepted")
		}
		m.mu.Lock()
		delete(m.instances, task.ConversationID)
		m.mu.Unlock()
		lease, e := m.AcquireDeliveryLease(ctx, d.ID, "task")
		if e != nil {
			t.Fatal("rejected writer blocked retry", e)
		}
		releaseLease(t, lease, true)
	})

	t.Run("unknown launch window stays closed", func(t *testing.T) {
		m, _, x, d, _ := deliveryLeaseFixture(t)
		lease, e := m.AcquireDeliveryLease(ctx, d.ID, "task")
		if e != nil {
			t.Fatal(e)
		}
		if e = lease.Attempt(); e != nil {
			t.Fatal(e)
		}
		if e = lease.Release(false); e != nil {
			t.Fatal(e)
		}
		if again, e := m.AcquireDeliveryLease(ctx, d.ID, "task"); e == nil {
			_ = again.Release(true)
			t.Fatal("unknown launch ownership silently discarded")
		}
		if e := os.Remove(filepath.Join(filepath.Dir(x.Workspace), "process-owner.json")); e != nil {
			t.Fatal(e)
		}
		if again, e := m.AcquireDeliveryLease(ctx, d.ID, "task"); e != nil {
			t.Fatal("fixture cannot recover", e)
		} else {
			releaseLease(t, again, true)
		}
	})

	t.Run("live foreign writer rejected", func(t *testing.T) {
		m, _, x, d, _ := deliveryLeaseFixture(t)
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(exe, "-test.run=^TestDeliveryLingeringSupport$", "--", "wait")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()
		boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if e != nil {
			t.Fatal(e)
		}
		marker := map[string]any{"Boot": strings.TrimSpace(string(boot)), "Group": cmd.Process.Pid}
		b, _ := json.Marshal(marker)
		if e = os.WriteFile(filepath.Join(filepath.Dir(x.Workspace), "process-owner.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = m.AcquireDeliveryLease(ctx, d.ID, "task"); e == nil {
			t.Fatal("foreign writer accepted")
		}
	})

	t.Run("idle session recycled and workspace handed over", func(t *testing.T) {
		m, task, x, d, messageID := deliveryLeaseFixture(t)
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		m.cfg.Program = exe
		m.cfg.BaseArgs = []string{"-test.run=TestFakePi", "--"}
		m.cfg.ExtraEnv = []string{"KAIWU_AGENT_HELPER=1"}
		if e = m.Send(task.ConversationID, messageID); e != nil {
			t.Fatal(e)
		}
		waitIdle(t, m, task.ConversationID)
		before := m.store.Snapshot()
		if m.Count() != 1 {
			t.Fatalf("idle session missing: %d", m.Count())
		}
		session := before.Conversations[task.ConversationID]
		if session.SessionID == "" || session.SessionFile == "" {
			t.Fatal("fixture session identity missing")
		}
		lease, e := m.AcquireDeliveryLease(ctx, d.ID, "task")
		if e != nil {
			t.Fatal(e)
		}
		if m.Count() != 0 {
			t.Fatalf("idle session not recycled: %d", m.Count())
		}
		after := m.store.Snapshot()
		if after.Conversations[task.ConversationID].SessionID != session.SessionID || after.Conversations[task.ConversationID].SessionFile != session.SessionFile {
			t.Fatal("session identity lost during handoff")
		}
		if _, e = os.Stat(x.Workspace); e != nil {
			t.Fatal("workspace lost during handoff", e)
		}
		releaseLease(t, lease, true)
	})
}

// go:build line above excludes other platforms; helper keeps cmd build simple.
func TestDeliveryLingeringSupport(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		t.Skip("lingering")
	}
	time.Sleep(2 * time.Minute)
}
