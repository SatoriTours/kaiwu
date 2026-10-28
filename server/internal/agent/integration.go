package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"sync"
	"time"
)

func (m *Manager) ensureMain(ctx context.Context, projectID string) (workbench.Project, error) {
	p, ok := m.store.Snapshot().Projects[projectID]
	if !ok {
		return p, workbench.Missing()
	}
	if p.DefaultBranch == "" && p.MainBranchOverride == "" {
		b, e := gitops.DetectMain(ctx, p.Path)
		if e != nil {
			return p, e
		}
		e = m.store.Update(func(st *workbench.State) error {
			current, ok := st.Projects[projectID]
			if !ok {
				return workbench.Missing()
			}
			if current.DefaultBranch == "" {
				current.DefaultBranch = b
				st.Projects[projectID] = current
			}
			p = current
			return nil
		})
		if e != nil {
			return p, e
		}
	}
	if !p.MainSeen {
		seen := false
		if _, err := gitops.Head(ctx, p.Path, "refs/heads/"+p.DefaultBranch); err == nil {
			seen = true
		}
		if !seen {
			st := m.store.Snapshot()
			for _, t := range st.Tasks {
				if t.ProjectID == p.ID && st.Executions[t.ExecutionID].BaseSHA != "" {
					seen = true
					break
				}
			}
		}
		if seen {
			if err := m.store.Update(func(st *workbench.State) error {
				v := st.Projects[p.ID]
				v.MainSeen = true
				st.Projects[p.ID] = v
				p = v
				return nil
			}); err != nil {
				return p, err
			}
		}
	}
	return p, nil
}

// Store admission precedes this reservation. No Store lock calls back into Manager.
func (m *Manager) reserveTask(ctx context.Context, taskID string) (func() error, error) {
	task, ok := m.store.Snapshot().Tasks[taskID]
	if !ok {
		return nil, workbench.Missing()
	}
	m.mu.Lock()
	if m.closed || m.reserved[taskID] || m.reserved["project:"+task.ProjectID] {
		m.mu.Unlock()
		return nil, workbench.Conflict("任务正在集成")
	}
	entry := m.instances[task.ConversationID]
	if entry != nil && entry.busy {
		m.mu.Unlock()
		return nil, workbench.Conflict("任务执行中，请等待结束")
	}
	m.reserved[taskID] = true
	var clientDone chan struct{}
	if entry != nil {
		entry.dead = true
		clientDone = entry.consumerDone
	}
	m.mu.Unlock()
	clear := func() { m.mu.Lock(); delete(m.reserved, taskID); m.mu.Unlock() }
	if entry != nil && entry.client != nil {
		cleanup, cancel := context.WithTimeout(ctx, 8*time.Second)
		e := entry.client.Close(cleanup)
		result, waitErr := entry.client.Wait(cleanup)
		cancel()
		if e != nil || waitErr != nil || !result.Reaped || result.CleanupError != nil {
			clear()
			return nil, errors.New("原 pi 进程尚未可靠回收，未接管工作区")
		}
		if clientDone != nil {
			select {
			case <-clientDone:
			case <-ctx.Done():
				clear()
				return nil, ctx.Err()
			}
		}
	}
	m.mu.Lock()
	if entry != nil {
		if entry.lease != nil {
			_ = entry.lease.release(true)
			entry.lease = nil
		}
		delete(m.instances, task.ConversationID)
	}
	m.mu.Unlock()
	var once sync.Once
	return func() error { once.Do(clear); return nil }, nil
}

// PreviewMergeReview produces a local review proof bound to the newest finished
// round. The proof carries the mode and round so BeginMerge can acknowledge that
// exact round in the same atomic write that queues the merge. Push is named but
// not executable in this slice.
func (m *Manager) PreviewMergeReview(ctx context.Context, review workbench.MergeReviewRequest) (workbench.MergePreview, error) {
	if review.Mode != workbench.MergeModeLocal {
		return workbench.MergePreview{}, workbench.Conflict("推送合并尚未开通，本版本只支持本地合并")
	}
	proof, e := m.previewMerge(ctx, review.ProjectID, review.TaskID)
	if e != nil {
		return proof, e
	}
	// Admission re-checks the same evidence; failing here keeps the preview from
	// ever advertising an unconfirmable round.
	if _, e := workbench.ReviewedTask(m.store.Snapshot(), workbench.MergeRequest{ProjectID: review.ProjectID, TaskID: review.TaskID, Mode: review.Mode, RoundID: review.RoundID}); e != nil {
		return workbench.MergePreview{}, e
	}
	proof.Mode, proof.RoundID = review.Mode, review.RoundID
	// The stored preview is what StartMerge re-reads, so the binding has to be
	// written back before the caller can queue the merge.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return proof, errors.New("manager closed")
	}
	if stored, ok := m.previews[proof.ID]; ok {
		stored.Mode, stored.RoundID = proof.Mode, proof.RoundID
		m.previews[proof.ID] = stored
	}
	return proof, nil
}

// PreviewMerge is the legacy, unreviewed entry: same signature and behaviour as
// before, including the fact that it never acknowledges a round.
func (m *Manager) PreviewMerge(ctx context.Context, projectID, taskID string) (workbench.MergePreview, error) {
	return m.previewMerge(ctx, projectID, taskID)
}

func (m *Manager) previewMerge(ctx context.Context, projectID, taskID string) (workbench.MergePreview, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return workbench.MergePreview{}, errors.New("manager closed")
	}
	select {
	case m.gitSlots <- struct{}{}:
	default:
		m.mu.Unlock()
		return workbench.MergePreview{}, workbench.Conflict("Git 检查繁忙，请稍后重试")
	}
	m.wg.Add(1)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel(); <-m.gitSlots; m.wg.Done() }()

	p, e := m.ensureMain(ctx, projectID)
	if e != nil {
		return workbench.MergePreview{}, e
	}
	st := m.store.Snapshot()
	t, ok := st.Tasks[taskID]
	if !ok || t.ProjectID != projectID {
		return workbench.MergePreview{}, workbench.Missing()
	}
	proof, e := gitops.Preview(ctx, p, t, st.Executions[t.ExecutionID], m.cfg.DataDir)
	if e != nil {
		return proof, e
	}
	if c := st.Conversations[t.ConversationID]; c.RunState == "starting" || c.RunState == "running" || workbench.TaskMerging(st, t.ID) {
		proof.CanMerge = false
		proof.Reason = "任务执行或合并中，请等待结束"
	}
	proof.ID = randomID()
	proof.ExpiresAt = time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return proof, errors.New("manager closed")
	}
	// Previews and their remote reviews share one lifetime: a dropped preview
	// must drop its cached review too, otherwise the review map grows without
	// bound while silently outliving what it reviews.
	expired := 0
	for id, v := range m.previews {
		expire, _ := time.Parse(time.RFC3339, v.ExpiresAt)
		if time.Now().After(expire) {
			delete(m.previews, id)
			delete(m.reviews, id)
			expired++
		}
	}
	if len(m.previews) >= 256 {
		return proof, workbench.Conflict("预检数量已达上限，请稍后重试")
	}
	m.previews[proof.ID] = proof
	return proof, nil
}

// dropPreview removes a preview and any review bound to it, keeping the two
// caches consistent after a terminal operation or an eviction.
func (m *Manager) dropPreview(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.previews, id)
	delete(m.reviews, id)
}
func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (m *Manager) projectGate(common string) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.projectGates == nil {
		m.projectGates = map[string]chan struct{}{}
	}
	gate := m.projectGates[common]
	if gate == nil {
		gate = make(chan struct{}, 1)
		m.projectGates[common] = gate
	}
	return gate
}
func (m *Manager) runMerge(op workbench.MergeOperation) {
	phase := func(name string, change func(*workbench.MergeOperation)) error {
		return m.store.AdvanceMerge(op.ID, func(x *workbench.MergeOperation) error {
			x.Phase = name
			if change != nil {
				change(x)
			}
			return nil
		})
	}
	// Publish a terminal phase only after all leases/reservations have been released.
	finalPhase, finalError, finalMain := "needs_check", "阶段未可靠保存，请核查保留的工作区", ""
	defer func() {
		_ = phase(finalPhase, func(x *workbench.MergeOperation) {
			x.Error = finalError
			x.MainAfterSHA = finalMain
			x.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
	}()
	fail := func(e error) {
		finalPhase = "failed"
		if m.ctx.Err() != nil {
			finalPhase = "needs_check"
		}
		finalError = e.Error()
	}
	ctx := m.ctx
	p, e := m.ensureMain(ctx, op.ProjectID)
	if e != nil {
		fail(e)
		return
	}
	common, e := gitops.CommonDir(ctx, p.Path)
	if e != nil {
		fail(e)
		return
	}
	gate := m.projectGate(common)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		fail(ctx.Err())
		return
	}
	projectLease, e := acquireProjectMergeLease(common)
	if e != nil {
		fail(e)
		return
	}
	safe := true
	defer func() { _ = projectLease.release(safe) }()
	if e = phase("preparing", nil); e != nil {
		return
	}
	if e = m.stopTaskPreview(ctx, op.TaskID); e != nil {
		fail(e)
		return
	}
	release, e := m.reserveTask(ctx, op.TaskID)
	if e != nil {
		fail(e)
		return
	}
	defer release()
	st := m.store.Snapshot()
	t := st.Tasks[op.TaskID]
	x := st.Executions[t.ExecutionID]
	taskLease, e := acquireExecutionLease(x.Workspace)
	if e != nil {
		fail(e)
		return
	}
	defer func() { _ = taskLease.release(safe) }()
	fresh, e := gitops.Preview(ctx, p, t, x, m.cfg.DataDir)
	if e != nil {
		fail(e)
		return
	}
	if !fresh.CanMerge || fresh.Fingerprint != op.Fingerprint {
		fail(errors.New("工作区、主分支或设置已变化，请重新预检并确认。" + fresh.Reason))
		return
	}
	runner := func(ctx context.Context, dir string, args ...string) (string, error) {
		if e := errors.Join(projectLease.attempt(), taskLease.attempt()); e != nil {
			return "", errors.New("无法保存 Git 启动归属，未执行命令")
		}
		pid := 0
		out, e := gitops.Run(ctx, dir, func(id int) error { pid = id; return errors.Join(projectLease.started(id), taskLease.started(id)) }, args...)
		if pid == 0 || !processGroupGone(pid) {
			safe = false
		}
		return out, e
	}
	sha := fresh.TaskSHA
	if fresh.NeedsCommit {
		if e = phase("committing", nil); e != nil {
			return
		}
		sha, e = gitops.CommitTask(ctx, x.Workspace, op.CommitMessage, fresh.CommitName, fresh.CommitEmail, runner, fresh)
		if e != nil {
			fail(e)
			return
		}
		if e = phase("committed", func(op *workbench.MergeOperation) { op.TaskAfterSHA = sha }); e != nil {
			return
		}
	}
	if e = phase("merging", func(op *workbench.MergeOperation) { op.TaskAfterSHA = sha }); e != nil {
		return
	}
	mainSHA, e := gitops.MergeTask(ctx, p, fresh, sha, runner)
	if e != nil {
		fail(e)
		_ = m.RefreshGit(ctx, p.ID)
		return
	}
	status, e := gitops.Check(ctx, p, t, x, m.cfg.DataDir)
	if e != nil || status.State != "merged" {
		fail(errors.New("Git 操作结束，但无法确认全部成果已合并，请核查"))
		return
	}
	_ = m.store.SaveGitStatus(status)
	if fresh.MainBranch == p.DefaultBranch {
		_ = m.store.Update(func(st *workbench.State) error {
			v := st.Projects[p.ID]
			v.MainSeen = true
			st.Projects[p.ID] = v
			return nil
		})
	}
	finalPhase, finalError, finalMain = "succeeded", "", mainSHA
}
