package agent

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func integrationFixture(t *testing.T) (*Manager, workbench.Task, workbench.Execution, string) {
	t.Helper()
	m, task, x, source := workspaceFixture(t)
	os.WriteFile(filepath.Join(source, "base.txt"), []byte("base"), 0600)
	workspaceGit(t, source, "add", ".")
	workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
	x, e := m.prepareWorkspace(context.Background(), task, x)
	if e != nil {
		t.Fatal(e)
	}
	task.ExecutionID = x.ID
	_ = m.store.Update(func(st *workbench.State) error {
		st.Tasks[task.ID] = task
		st.Executions[x.ID] = x
		st.Conversations[task.ConversationID] = workbench.Conversation{ID: task.ConversationID, ProjectID: task.ProjectID, TaskID: task.ID, RunState: "idle"}
		return nil
	})
	return m, task, x, source
}
func TestProjectMergeLockCoversRepositoryAliases(t *testing.T) {
	m, _, _, source := integrationFixture(t)
	_ = m
	common := filepath.Join(source, ".git")
	a, e := acquireProjectMergeLease(common)
	if e != nil {
		t.Fatal(e)
	}
	defer a.release(true)
	if b, e := acquireProjectMergeLease(common); e == nil {
		b.release(true)
		t.Fatal("concurrent writer accepted")
	}
}
func TestMergeReservationRejectsBusyWriter(t *testing.T) {
	m, task, _, _ := integrationFixture(t)
	m.instances[task.ConversationID] = &instance{busy: true}
	if release, e := m.reserveTask(context.Background(), task.ID); e == nil {
		release()
		t.Fatal("busy writer accepted")
	}
}
func waitMerge(t *testing.T, m *Manager, id string) workbench.MergeOperation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op := m.store.Snapshot().Merges[id]
		if !workbench.MergeActive(op.Phase) {
			return op
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("merge did not settle")
	return workbench.MergeOperation{}
}
func TestMergeTaskEndToEnd(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "feature.txt"), []byte("feature"), 0600)
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !p.CanMerge {
		t.Fatal(p, e)
	}
	req := workbench.MergeRequest{OperationID: "merge-feature", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "feature"}
	op, replay, e := m.StartMerge(req)
	if e != nil || replay {
		t.Fatal(op, e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "succeeded" {
		t.Fatal(op)
	}
	if b, e := os.ReadFile(filepath.Join(source, "feature.txt")); e != nil || string(b) != "feature" {
		t.Fatal("not integrated", e)
	}
	_, replay, e = m.StartMerge(req)
	if e != nil || !replay {
		t.Fatal("lost idempotency", e)
	}
	if _, e = os.Stat(x.Workspace); e != nil {
		t.Fatal("worktree removed")
	}
}
func TestMergeRejectsUnsafeMainAndStalePreview(t *testing.T) {
	for _, mode := range []string{"main_dirty", "stale"} {
		t.Run(mode, func(t *testing.T) {
			m, task, x, source := integrationFixture(t)
			os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("one"), 0600)
			p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
			if e != nil || !p.CanMerge {
				t.Fatal(p, e)
			}
			if mode == "stale" {
				os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("two"), 0600)
			} else {
				os.WriteFile(filepath.Join(source, "external"), []byte("preserve"), 0600)
			}
			op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "merge-stale", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "feature"})
			if e == nil {
				op = waitMerge(t, m, op.ID)
				if op.Phase == "succeeded" {
					t.Fatal("unsafe merge accepted")
				}
			}
			if workspaceGit(t, x.Workspace, "rev-parse", "HEAD") != x.BaseSHA {
				t.Fatal("unreviewed files committed")
			}
		})
	}
}
func TestMergeFailurePreservesPartialResult(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "base.txt"), []byte("task"), 0600)
	os.WriteFile(filepath.Join(source, "base.txt"), []byte("main"), 0600)
	workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-am", "main")
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !p.CanMerge {
		t.Fatal(p, e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "merge-conflict", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "task"})
	if e != nil {
		t.Fatal(e)
	}
	op = waitMerge(t, m, op.ID)
	if op.Phase != "failed" || op.TaskAfterSHA == "" {
		t.Fatal(op)
	}
	if b, _ := os.ReadFile(filepath.Join(source, "base.txt")); string(b) != "main" {
		t.Fatal("main changed")
	}
	if _, e = os.Stat(filepath.Join(source, ".git", "MERGE_HEAD")); !os.IsNotExist(e) {
		t.Fatal("conflict modified main index")
	}
}
func TestFirstMergeIntoUnbornMain(t *testing.T) {
	for _, seed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "seed_preserved"}[seed], func(t *testing.T) {
			m, task, x, source := workspaceFixture(t)
			if seed {
				os.WriteFile(filepath.Join(source, "seed"), []byte("original"), 0600)
			}
			x, e := m.prepareWorkspace(context.Background(), task, x)
			if e != nil {
				t.Fatal(e)
			}
			task.ExecutionID = x.ID
			_ = m.store.Update(func(st *workbench.State) error {
				st.Tasks[task.ID] = task
				st.Executions[x.ID] = x
				st.Conversations[task.ConversationID] = workbench.Conversation{ID: task.ConversationID, RunState: "idle"}
				return nil
			})
			os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
			p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
			if e != nil {
				t.Fatal(e)
			}
			if seed {
				if p.CanMerge || p.Reason == "" {
					t.Fatal("seed overwritten", p)
				}
				if b, _ := os.ReadFile(filepath.Join(source, "seed")); string(b) != "original" {
					t.Fatal("seed changed")
				}
				return
			}
			op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "first-merge", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "first feature"})
			if e != nil {
				t.Fatal(e)
			}
			op = waitMerge(t, m, op.ID)
			if op.Phase != "succeeded" {
				t.Fatal(op)
			}
			if _, e = os.Stat(filepath.Join(source, "feature")); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestMergeReservationPreservesSession(t *testing.T) {
	m, task, x, _ := integrationFixture(t)
	exe, _ := os.Executable()
	m.cfg.Program = exe
	m.cfg.BaseArgs = []string{"-test.run=TestFakePi", "--"}
	m.cfg.ExtraEnv = []string{"KAIWU_AGENT_HELPER=1"}
	_ = m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[task.ConversationID]
		c.SessionID = "kaiwu_merge_fixture"
		c.Profile = workbench.Profile{Provider: "fake", Model: "fake"}
		st.Conversations[c.ID] = c
		return nil
	})
	send := func(key string) {
		t.Helper()
		r, e := m.store.Command(workbench.Command{OperationID: key, Type: "send_message", ProjectID: task.ProjectID, ConversationID: task.ConversationID, Text: key})
		if e != nil {
			t.Fatal(e)
		}
		if e = m.Send(task.ConversationID, r.CreatedID); e != nil {
			t.Fatal(e)
		}
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			c := m.store.Snapshot().Conversations[task.ConversationID]
			if c.RunState == "idle" {
				return
			}
			if c.RunState == "failed" || c.RunState == "interrupted" {
				t.Fatal(c.Error)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("pi did not settle")
	}
	send("before-merge")
	before := m.store.Snapshot().Conversations[task.ConversationID]
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "session-merge", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "feature"})
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatal(op)
	}
	send("after-merge")
	after := m.store.Snapshot().Conversations[task.ConversationID]
	if before.SessionID != after.SessionID || before.SessionFile != after.SessionFile || len(after.Messages) != 4 {
		t.Fatal("original context replaced or replayed", after)
	}
}
func TestMergeIdempotencyAcrossRestart(t *testing.T) {
	m, task, x, _ := integrationFixture(t)
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	req := workbench.MergeRequest{OperationID: "restart-merge", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p.ID, CommitMessage: "feature"}
	op, _, e := m.StartMerge(req)
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatal(op)
	}
	_ = m.store.AdvanceMerge(op.ID, func(o *workbench.MergeOperation) error { o.Phase = "merging"; return nil })
	m.Close()
	m.store.Close()
	s, e := workbench.Open(filepath.Join(m.cfg.DataDir, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m2 := New(context.Background(), s, m.cfg)
	defer m2.Close()
	if s.Snapshot().Merges[op.ID].Phase != "needs_check" {
		t.Fatal("not uncertain")
	}
	headBefore := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	if e = m2.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	op, replay, e := m2.StartMerge(req)
	if e != nil || !replay || op.Phase != "succeeded" {
		t.Fatal(op, replay, e)
	}
	if workspaceGit(t, x.Workspace, "rev-parse", "HEAD") != headBefore {
		t.Fatal("recovery repeated commit")
	}
}
func TestGitRefreshIsBoundedAndChangeDriven(t *testing.T) {
	m, task, _, _ := integrationFixture(t)
	if e := m.RefreshGit(context.Background(), "p"); e != nil {
		t.Fatal(e)
	}
	before := m.store.Snapshot().Revision
	if e := m.RefreshGit(context.Background(), "p"); e != nil {
		t.Fatal(e)
	}
	if m.store.Snapshot().Revision != before {
		t.Fatal("unchanged check wrote state")
	}
	m.gitSlots <- struct{}{}
	m.gitSlots <- struct{}{}
	defer func() { <-m.gitSlots; <-m.gitSlots }()
	if e := m.RefreshGit(context.Background(), "p"); e == nil {
		t.Fatal("unbounded check accepted")
	}
	if _, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID); e == nil {
		t.Fatal("preview bypassed bounded checks")
	}
}
func TestUnbornMainAcceptsAlreadyCommittedTask(t *testing.T) {
	m, task, x, _ := workspaceFixture(t)
	x, e := m.prepareWorkspace(context.Background(), task, x)
	if e != nil {
		t.Fatal(e)
	}
	task.ExecutionID = x.ID
	_ = m.store.Update(func(st *workbench.State) error {
		st.Tasks[task.ID] = task
		st.Executions[x.ID] = x
		st.Conversations[task.ConversationID] = workbench.Conversation{ID: task.ConversationID, RunState: "idle"}
		return nil
	})
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("committed externally"), 0600)
	workspaceGit(t, x.Workspace, "add", ".")
	workspaceGit(t, x.Workspace, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "feature")
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !p.CanMerge {
		t.Fatal("unborn main rejected committed task", p, e)
	}
}
func TestMergeTaskBranchesRemainIsolatedAndMainBaseFrozen(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	workspaceGit(t, source, "checkout", "-b", "other")
	os.WriteFile(filepath.Join(source, "other-only"), []byte("other"), 0600)
	workspaceGit(t, source, "add", ".")
	workspaceGit(t, source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "other")
	task.ID = strings.Repeat("b", 32)
	task.ConversationID = "second"
	y, e := m.prepareWorkspace(context.Background(), task, workbench.Execution{ID: "second-execution"})
	if e != nil {
		t.Fatal(e)
	}
	if y.BaseSHA != x.BaseSHA || y.Workspace == x.Workspace || y.Branch == x.Branch {
		t.Fatal("not based on frozen main", y, x)
	}
	if _, e = os.Stat(filepath.Join(y.Workspace, "other-only")); !os.IsNotExist(e) {
		t.Fatal("copied different branch file")
	}
}
func TestMergeDivergenceAndConcurrentCards(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	other := task
	other.ID = strings.Repeat("b", 32)
	other.ConversationID = "second"
	y, e := m.prepareWorkspace(context.Background(), other, workbench.Execution{ID: "second"})
	if e != nil {
		t.Fatal(e)
	}
	other.ExecutionID = y.ID
	_ = m.store.Update(func(st *workbench.State) error {
		st.Tasks[other.ID] = other
		st.Executions[y.ID] = y
		st.Conversations[other.ConversationID] = workbench.Conversation{ID: other.ConversationID, ProjectID: other.ProjectID, TaskID: other.ID, RunState: "idle"}
		return nil
	})
	for name, tree := range map[string]string{"one": x.Workspace, "two": y.Workspace} {
		os.WriteFile(filepath.Join(tree, name), []byte(name), 0600)
	}
	p1, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil {
		t.Fatal(e)
	}
	p2, e := m.PreviewMerge(context.Background(), other.ProjectID, other.ID)
	if e != nil {
		t.Fatal(e)
	}
	a, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "parallel-one", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: p1.ID, CommitMessage: "one"})
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "parallel-two", ProjectID: other.ProjectID, TaskID: other.ID, PreviewID: p2.ID, CommitMessage: "two"})
	if e != nil {
		t.Fatal(e)
	}
	a = waitMerge(t, m, a.ID)
	b = waitMerge(t, m, b.ID)
	if a.Phase == b.Phase || a.Phase != "succeeded" && b.Phase != "succeeded" {
		t.Fatal("expected one stale review after serialized merge", a, b)
	}
	pending := other
	if b.Phase == "succeeded" {
		pending = task
	}
	proof, e := m.PreviewMerge(context.Background(), pending.ProjectID, pending.ID)
	if e != nil || !proof.CanMerge {
		t.Fatal(proof, e)
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "rechecked-second", ProjectID: pending.ProjectID, TaskID: pending.ID, PreviewID: proof.ID, CommitMessage: "remaining feature"})
	if e != nil {
		t.Fatal(e)
	}
	if op = waitMerge(t, m, op.ID); op.Phase != "succeeded" {
		t.Fatal(op)
	}
	for _, name := range []string{"one", "two"} {
		if _, e = os.Stat(filepath.Join(source, name)); e != nil {
			t.Fatal("missing integrated card", e)
		}
	}
	if len(strings.Fields(workspaceGit(t, source, "rev-list", "--parents", "-n", "1", "HEAD"))) != 3 {
		t.Fatal("divergent merge did not retain both parents")
	}
}

func TestProjectGitSettingsOptionalAndValidated(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	frozen := m.store.Snapshot().Projects[task.ProjectID].DefaultBranch
	workspaceGit(t, source, "branch", "release")
	os.WriteFile(filepath.Join(x.Workspace, "feature"), []byte("feature"), 0600)
	proof, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || !proof.CanMerge {
		t.Fatal(proof, e)
	}
	set := func(id, branch string) error {
		_, err := m.UpdateProjectGit(context.Background(), workbench.Command{OperationID: id, Type: "update_project_git", Revision: m.store.Snapshot().Revision, ProjectID: task.ProjectID, MainBranchOverride: branch})
		return err
	}
	for i, branch := range []string{"missing", "bad..name", x.Branch} {
		if set("bad-setting-"+strings.Repeat("x", i+1), branch) == nil {
			t.Fatal("invalid override accepted", branch)
		}
	}
	if e = set("set-release", "release"); e != nil {
		t.Fatal(e)
	}
	if m.store.Snapshot().Executions[x.ID].BaseSHA != x.BaseSHA {
		t.Fatal("setting changed task baseline")
	}
	op, _, e := m.StartMerge(workbench.MergeRequest{OperationID: "old-setting-proof", ProjectID: task.ProjectID, TaskID: task.ID, PreviewID: proof.ID, CommitMessage: "feature"})
	if e == nil && waitMerge(t, m, op.ID).Phase == "succeeded" {
		t.Fatal("stale settings proof merged")
	}
	if e = set("clear-release", ""); e != nil {
		t.Fatal(e)
	}
	if m.store.Snapshot().Projects[task.ProjectID].DefaultBranch != frozen {
		t.Fatal("clearing override lost frozen main")
	}
	// When autodetection cannot choose a target, the advertised advanced setting
	// must still allow selecting an existing local branch.
	workspaceGit(t, source, "checkout", "--detach")
	workspaceGit(t, source, "branch", "-D", frozen)
	_ = m.store.Update(func(st *workbench.State) error {
		p := st.Projects[task.ProjectID]
		p.DefaultBranch = ""
		st.Projects[p.ID] = p
		return nil
	})
	if e = set("manual-fallback", "release"); e != nil {
		t.Fatal("manual main setting could not recover ambiguous repository", e)
	}
	if _, e = m.PreviewMerge(context.Background(), task.ProjectID, task.ID); e != nil {
		t.Fatal("manual target ignored by preview", e)
	}
}

func TestConfiguredServiceDataMappedIntoTask(t *testing.T) {
	m, task, x, source := integrationFixture(t)
	m.cfg.DataDir = filepath.Join(source, "state")
	os.MkdirAll(filepath.Join(m.cfg.DataDir, "sessions"), 0700)
	os.WriteFile(filepath.Join(source, ".git", "info", "exclude"), []byte("state/\n"), 0600)
	session := filepath.Join(x.Workspace, "state", "sessions", "fixture.jsonl")
	os.MkdirAll(filepath.Dir(session), 0700)
	os.WriteFile(session, []byte("fixture private session"), 0600)
	p, e := m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || p.CanMerge || !strings.Contains(p.Reason, "私密") {
		t.Fatal("mapped data directory not protected", p, e)
	}
	workspaceGit(t, x.Workspace, "add", ".")
	workspaceGit(t, x.Workspace, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "private copy")
	workspaceGit(t, x.Workspace, "rm", "state/sessions/fixture.jsonl")
	workspaceGit(t, x.Workspace, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "remove private copy")
	p, e = m.PreviewMerge(context.Background(), task.ProjectID, task.ID)
	if e != nil || p.CanMerge || !strings.Contains(p.Reason, "私密") {
		t.Fatal("historical mapped data directory not protected", p, e)
	}
}
