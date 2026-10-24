//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func spawnLeasedChild(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd, cmd.Process.Pid
}

// A remote writer whose process group was not confirmed gone must keep the
// durable ownership marker, so a new writer is refused. Only a confirmed exit
// frees the workspace. Both halves use a real controlled process.
func TestUnprovenWriterGroupBlocksNewOwner(t *testing.T) {
	root := t.TempDir()
	lease, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatal(e)
	}
	child, pid := spawnLeasedChild(t)
	if e := lease.attempt(); e != nil {
		t.Fatal(e)
	}
	if e := lease.started(pid); e != nil {
		t.Fatal(e)
	}
	// Simulate a crash: the OS lock is dropped without a confirmed clean exit.
	if e := lease.file.Close(); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "kaiwu-merge-owner.json")
	if _, e := os.Stat(marker); e != nil {
		t.Fatalf("unconfirmed release dropped ownership: %v", e)
	}
	if again, e := acquireProjectMergeLease(root); e == nil {
		_ = again.release(false)
		t.Fatal("new writer admitted while the old group was still alive")
	}
	// Confirmed exit frees it.
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
	again, e := acquireProjectMergeLease(root)
	if e != nil {
		t.Fatalf("confirmed clean exit did not free the workspace: %v", e)
	}
	if e := again.release(true); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("clean release left ownership: %v", e)
	}
}

// Terminal success may never be published when the writer group is unproven:
// the operation degrades to needs_check and keeps its audit facts.
func TestFinishPushRefusesSuccessOnUnsafeRelease(t *testing.T) {
	m, task, _, _, _ := pushFixture(t)
	review := pushReview(t, m, task)
	op, _, e := m.store.BeginMergeReview(workbench.MergeRequest{
		OperationID: "unsafe-release", ProjectID: task.ProjectID, TaskID: task.ID,
		PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1",
	}, review)
	if e != nil {
		t.Fatal(e)
	}
	_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
		v.Phase = "remote_done"
		v.IntendedSHA = review.LocalMainSHA
		v.RemoteAfterSHA = review.LocalMainSHA
		return nil
	})
	m.finishPush(op.ID, "", "", false)
	unsafe := m.store.Snapshot().Merges[op.ID]
	if unsafe.Phase != "needs_check" || unsafe.RemoteAfterSHA == "" || unsafe.Error == "" {
		t.Fatalf("unsafe release published success: %#v", unsafe)
	}
	// The operation no longer claims to be active, so a human can resolve it, but
	// the workspace ownership itself is what keeps a new writer out (proven by
	// TestUnprovenWriterGroupBlocksNewOwner with a real surviving process group).
	if workbench.TaskMerging(m.store.Snapshot(), task.ID) {
		t.Fatal("unsafe release left the operation blocking the task forever")
	}
	// A later success write may not erase the recorded needs_check cause.
	m.finishPush(op.ID, "", "", true)
	if again := m.store.Snapshot().Merges[op.ID]; again.Phase != "needs_check" || again.Error == "" {
		t.Fatalf("needs_check cause erased: %#v", again)
	}
	// The same facts with a proven release do succeed on a fresh operation, so
	// the refusal is about the group proof and not about the phase.
	op2, _, e := m.store.BeginMergeReview(workbench.MergeRequest{
		OperationID: "unsafe-release-ok", ProjectID: task.ProjectID, TaskID: task.ID,
		PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1",
	}, review)
	if e != nil {
		t.Fatal(e)
	}
	_ = m.store.AdvanceMerge(op2.ID, func(v *workbench.MergeOperation) error {
		v.Phase = "remote_done"
		v.IntendedSHA = review.LocalMainSHA
		v.RemoteAfterSHA = review.LocalMainSHA
		return nil
	})
	m.finishPush(op2.ID, "", "", true)
	if ok := m.store.Snapshot().Merges[op2.ID]; ok.Phase != "succeeded" {
		t.Fatalf("%#v", ok)
	}
}

// A crash right after the real local merge may not lose the audit facts, and
// replaying the operation may not push a second time.
func TestCrashedMergedPhaseKeepsLocalFacts(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	op, _, e := m.store.BeginMergeReview(workbench.MergeRequest{
		OperationID: "crashed-merged", ProjectID: task.ProjectID, TaskID: task.ID,
		PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1",
	}, review)
	if e != nil {
		t.Fatal(e)
	}
	// Really merge locally, then persist nothing: the process died here.
	fresh, e := m.store.Snapshot().Merges[op.ID], error(nil)
	_ = fresh
	op0 := m.store.Snapshot().Merges[op.ID]
	if op0.Phase != "queued" {
		t.Fatalf("%#v", op0)
	}
	// A real non-fast-forward merge, so main really moves off the reviewed base.
	mergeOut, e := exec.Command("git", "-C", source, "-c", "user.name=F", "-c", "user.email=f@example.invalid",
		"merge", "--no-ff", "--no-edit", "-m", "Merge task", review.TaskSHA).CombinedOutput()
	if e != nil {
		t.Fatalf("fixture merge: %s %v", mergeOut, e)
	}
	wsHead := workspaceGit(t, x.Workspace, "rev-parse", "HEAD")
	mainHead := workspaceGit(t, source, "rev-parse", "HEAD")
	if wsHead == "" || mainHead == review.TaskSHA || mainHead == review.LocalMainSHA {
		t.Fatalf("fixture: %s %s", wsHead, mainHead)
	}
	_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
		v.Phase = "merged"
		return nil
	})
	remoteBefore := bareRef(t, conn.bare, review.MainRef)
	if e := m.RecoverMerges(t.Context()); e != nil {
		t.Fatal(e)
	}
	after := m.store.Snapshot().Merges[op.ID]
	if after.MainAfterSHA != mainHead || after.TaskAfterSHA != wsHead {
		t.Fatalf("local facts lost: %#v main=%s task=%s", after, mainHead, wsHead)
	}
	if after.Phase == "succeeded" || after.RemoteAfterSHA != "" {
		t.Fatalf("unverified remote success invented: %#v", after)
	}
	if conn.count() != 0 || bareRef(t, conn.bare, review.MainRef) != remoteBefore {
		t.Fatal("recovery pushed again")
	}
	if workbench.TaskMerging(m.store.Snapshot(), task.ID) {
		t.Fatal("recovered operation still blocks the task")
	}
}

// A failed persist of the local merge result may not silently lose it.
func TestLocalMergeFactSurvivesPersistFailure(t *testing.T) {
	m, task, _, source, conn := pushFixture(t)
	review := pushReview(t, m, task)
	failures := 0
	m.store.SetPersistBarrier(func(st workbench.State) error {
		for _, op := range st.Merges {
			if op.Phase == "merged" && op.MainAfterSHA != "" {
				failures++
				return os.ErrPermission
			}
		}
		return nil
	})
	t.Cleanup(func() { m.store.SetPersistBarrier(nil) })
	op := startPush(t, m, task, review, "lost-persist")
	if failures == 0 {
		t.Fatal("barrier never fired")
	}
	if op.IntendedSHA != "" || op.RemoteAfterSHA != "" || conn.count() != 0 {
		t.Fatalf("saved-fact failure still pushed: %#v pushes=%d", op, conn.count())
	}
	main := workspaceGit(t, source, "rev-parse", "HEAD")
	if main == review.LocalMainSHA || main == "" {
		t.Fatalf("fixture merge did not move main: %s", main)
	}
	// Recovery is observation-only and must recover the lost fact from Git.
	if e := m.RecoverMerges(t.Context()); e != nil {
		t.Fatal(e)
	}
	after := m.store.Snapshot().Merges[op.ID]
	if after.MainAfterSHA != main || after.IntendedSHA != "" || after.RemoteAfterSHA != "" || conn.count() != 0 {
		t.Fatalf("recovered fact wrong: %#v pushes=%d", after, conn.count())
	}
	if after.Phase == "succeeded" {
		t.Fatalf("unpushed merge reported as succeeded: %#v", after)
	}
	if workbench.TaskMerging(m.store.Snapshot(), task.ID) {
		t.Fatal("recovered operation still blocks the task")
	}
}

// Reviews must not outlive the preview they were bound to.
func TestReviewCacheEvictedWithPreview(t *testing.T) {
	m, task, _, _, _ := pushFixture(t)
	old := pushReview(t, m, task)
	expired := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	m.mu.Lock()
	p := m.previews[old.ID]
	p.ExpiresAt = expired
	m.previews[old.ID] = p
	m.mu.Unlock()
	if _, e := m.PreviewPushReview(t.Context(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"}); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	_, stayed := m.reviews[old.ID]
	m.mu.Unlock()
	if stayed {
		t.Fatal("review cache outlived its preview")
	}
}

// Eviction must not break concurrent readers: a review dropped for expiry may
// never corrupt the cache or expose a partially written entry.
func TestReviewCacheConcurrentReadDuringEviction(t *testing.T) {
	m, task, _, _, _ := pushFixture(t)
	keep := pushReview(t, m, task)
	if _, e := m.PreviewPushReview(context.Background(), workbench.MergeReviewRequest{ProjectID: task.ProjectID, TaskID: task.ID, Mode: workbench.MergeModePush, RoundID: "round-1"}); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	_, seeded := m.reviews[keep.ID]
	m.mu.Unlock()
	if !seeded {
		t.Fatal("review not cached")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 2000; n++ {
				m.mu.Lock()
				r, ok := m.reviews[keep.ID]
				_, pv := m.previews[keep.ID]
				m.mu.Unlock()
				if ok && r.ID == "" {
					t.Error("half-published review")
					return
				}
				// A review may never outlive its preview.
				if ok && !pv {
					t.Error("review cached without its preview")
					return
				}
			}
		}()
	}
	for i := 0; i < 50; i++ {
		m.dropPreview(keep.ID)
	}
	wg.Wait()
	m.mu.Lock()
	_, stayed := m.reviews[keep.ID]
	m.mu.Unlock()
	if stayed {
		t.Fatal("dropPreview left the review behind")
	}
}

// A task commit that really happened is a fact. If its record cannot be saved,
// the operation must keep the real SHA and stay honest, and it must not push.
func TestCommittedFactSurvivesPersistFailure(t *testing.T) {
	m, task, x, source, conn := pushFixture(t)
	if e := os.WriteFile(filepath.Join(x.Workspace, "more.txt"), []byte("more"), 0600); e != nil {
		t.Fatal(e)
	}
	review := pushReview(t, m, task)
	blocked := 0
	m.store.SetPersistBarrier(func(st workbench.State) error {
		for _, op := range st.Merges {
			if op.Phase == "committed" && op.TaskAfterSHA != "" {
				blocked++
				return os.ErrPermission
			}
		}
		return nil
	})
	t.Cleanup(func() { m.store.SetPersistBarrier(nil) })
	op := startPush(t, m, task, review, "lost-commit")
	if blocked == 0 {
		t.Fatal("barrier never blocked the commit record")
	}
	if op.IntendedSHA != "" || op.RemoteAfterSHA != "" || conn.count() != 0 {
		t.Fatalf("unsaved commit fact still pushed: %#v pushes=%d", op, conn.count())
	}
	if op.Phase != "needs_check" || op.Error == "" {
		t.Fatalf("unsaved commit fact not reported honestly: %#v", op)
	}
	if op.TaskAfterSHA == "" {
		t.Fatal("real task commit not recorded alongside the failure")
	}
	if got := workspaceGit(t, x.Workspace, "rev-parse", "HEAD"); got != op.TaskAfterSHA {
		t.Fatalf("recorded commit %s is not the real commit %s", op.TaskAfterSHA, got)
	}
	if got := workspaceGit(t, source, "log", "-1", "--format=%s"); strings.Contains(got, "Merge task") {
		t.Fatal("a commit that was never recorded still merged main")
	}
	// The fact must be durable, not just in-memory: close this instance first so
	// the database is not opened twice.
	m.store.SetPersistBarrier(nil)
	_ = m.Close()
	_ = m.store.Close()
	reopened, e := workbench.Open(filepath.Join(m.cfg.DataDir, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	saved := reopened.Snapshot().Merges[op.ID]
	if saved.Phase != "needs_check" || saved.TaskAfterSHA != op.TaskAfterSHA || saved.RemoteAfterSHA != "" {
		t.Fatalf("reopened state lost the commit fact: %#v", saved)
	}
}

// Recovery must obey the same writer-group contract as a normal push: an
// observation cannot publish success while the observed writer group survives.
func TestRecoveryKeepsOwnershipWhileGroupAlive(t *testing.T) {
	m, task, _, _, conn := pushFixture(t)
	review := pushReview(t, m, task)
	op, _, e := m.store.BeginMergeReview(workbench.MergeRequest{
		OperationID: "recover-live-group", ProjectID: task.ProjectID, TaskID: task.ID,
		PreviewID: review.ID, CommitMessage: "feature", Mode: workbench.MergeModePush, RoundID: "round-1",
	}, review)
	if e != nil {
		t.Fatal(e)
	}
	branch := m.store.Snapshot().Projects[task.ProjectID].DefaultBranch
	ref := "refs/heads/" + branch
	_ = m.store.AdvanceMerge(op.ID, func(v *workbench.MergeOperation) error {
		v.Phase = "pushing"
		v.IntendedSHA = review.LocalMainSHA
		v.TaskAfterSHA = review.TaskSHA
		v.MainAfterSHA = review.LocalMainSHA
		return nil
	})
	var child *exec.Cmd
	live := true
	runner := gitops.NewRemoteRunner(gitops.RemoteConfig{Transport: gitops.RemoteTransportFunc(func(_ context.Context, _ string, _ gitops.RemoteTarget, _ []string, _ string, lease gitops.ProcessLease) (string, error) {
		// The observation really starts a writer group and reports its identity.
		var pid int
		child2, pid := spawnLeasedChild(t)
		_ = child2
		if e := lease.Started(pid); e != nil {
			return "", e
		}
		child = child2
		if !live {
			_ = child2.Process.Kill()
			_, _ = child2.Process.Wait()
		}
		return review.LocalMainSHA + "\t" + ref + "\n", nil
	})})
	m.mu.Lock()
	m.cfg.Remote = runner
	m.mu.Unlock()
	if e := m.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	got := m.store.Snapshot().Merges[op.ID]
	if got.Phase != "needs_check" || got.RemoteAfterSHA != review.LocalMainSHA || got.Error == "" {
		t.Fatalf("live group still reported success: %#v", got)
	}
	if conn.count() != 0 {
		t.Fatalf("recovery pushed: %d", conn.count())
	}
	// The surviving group keeps ownership, so no second writer is accepted.
	common, e := gitops.CommonDir(context.Background(), m.store.Snapshot().Projects[task.ProjectID].Path)
	if e != nil {
		t.Fatal(e)
	}
	if other, e := acquireProjectMergeLease(common); e == nil {
		_ = other.release(false)
		t.Fatal("second writer admitted during live recovery group")
	}
	// A definitely finished group lets the same observation-only recovery finish
	// without ever pushing.
	live = false
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
	if e := m.RecoverMerges(context.Background()); e != nil {
		t.Fatal(e)
	}
	done := m.store.Snapshot().Merges[op.ID]
	if done.Phase != "succeeded" || done.RemoteAfterSHA != review.LocalMainSHA || conn.count() != 0 {
		t.Fatalf("proven-exit recovery wrong: %#v pushes=%d", done, conn.count())
	}
}
