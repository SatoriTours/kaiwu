package delivery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/githubactions"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// stagePace separates consecutive persisted stage transitions. Observers
// (Web API, operators) must actually see intermediate states such as
// committing or pushing_task; a fast chain would persist them for
// microseconds only. Pacing is cadence only: deadlines never use it.
var stagePace = 40 * time.Millisecond

// rescanInterval is the periodic arm between event-driven passes. CI waits,
// deadlines and revocations are re-evaluated on that cadence through the
// injected clock, never with real sleeps.
var rescanInterval = 15 * time.Second

// Manager drives the fixed automated delivery state machine. One worker
// goroutine scans persisted cycles, so no delivery stage ever runs
// concurrently with itself and the local/API concurrency ceilings are
// structurally satisfied.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	store *workbench.Store
	cfg   Config

	wake  chan struct{}
	revs  <-chan uint64
	unsub func()

	// leases/lastCI belong to the worker goroutine; Guarded by mu only so
	// Close can run concurrently with a blocked stage handler.
	mu         sync.Mutex
	leases     map[string]agent.DeliveryLease
	lastCI     map[string]string
	dispatched map[string]bool
	// userTurnAt maps a task to the delivery-clock start of its newest user
	// turn; it ends an excluded wait segment at the exact AI run start.
	userTurnAt map[string]time.Time
}

// New subscribes to store revisions and starts the delivery worker.
func New(ctx context.Context, store *workbench.Store, cfg Config) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("缺少状态库")
	}
	if cfg.Agent == nil || cfg.Actions == nil || cfg.Remote == nil || cfg.Clock == nil {
		return nil, fmt.Errorf("交付管理器缺少必需端口")
	}
	revs, unsub := store.Subscribe()
	owned, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx: owned, cancel: cancel, done: make(chan struct{}),
		store: store, cfg: cfg,
		wake:       make(chan struct{}, 1),
		revs:       revs,
		unsub:      unsub,
		leases:     map[string]agent.DeliveryLease{},
		lastCI:     map[string]string{},
		dispatched: map[string]bool{},
		userTurnAt: map[string]time.Time{},
	}
	// A user turn that starts a run ends the interaction wait at its real
	// start, not when the worker happens to observe it; this keeps AI runtime
	// out of the excluded wait window without resetting ActiveSince.
	store.SetUserTurnHook(m.NoteUserTurn)
	go m.loop()
	return m, nil
}

// Wake coalesces notifications: several wakes collapse into one pending
// pass, so the worker never races itself.
func (m *Manager) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Close stops accepting new passes, waits for the worker and releases every
// still-held write lease as unsafe (unverified outcome).
func (m *Manager) Close() error {
	m.cancel()
	<-m.done
	m.unsub()
	m.mu.Lock()
	held := m.leases
	m.leases = map[string]agent.DeliveryLease{}
	m.mu.Unlock()
	for _, l := range held {
		if l != nil {
			_ = l.Release(false)
		}
	}
	return nil
}

// Preflight verifies read-only readiness of one project's automation: repo
// identity, workflow presence and state. It never writes to Git or Actions.
func (m *Manager) Preflight(ctx context.Context, p workbench.Project, policy workbench.ProjectDeliveryPolicy) (Preflight, error) {
	pf := Preflight{Fingerprint: workbench.ProtectedDeliveryFingerprint(policy), Blockers: []string{}}
	if !policy.Enabled {
		pf.Blockers = append(pf.Blockers, "自动交付未启用")
		pf.Ready = len(pf.Blockers) == 0
		return pf, nil
	}
	info, err := m.cfg.Actions.Inspect(ctx, policy.Repository.ID, policy.Repository.Owner, policy.Repository.Name, policy.APICredentialRef)
	if err != nil {
		pf.Blockers = append(pf.Blockers, "仓库预检失败："+err.Error())
		pf.Ready = false
		return pf, nil
	}
	pf.Blockers = append(pf.Blockers, info.Blockers...)
	for _, wf := range policy.TaskWorkflows {
		if missingWorkflow(info.Workflows, wf.ID) {
			pf.Blockers = append(pf.Blockers, fmt.Sprintf("缺少任务分支 workflow %d", wf.ID))
		}
	}
	for _, wf := range policy.MainWorkflows {
		if missingWorkflow(info.Workflows, wf.ID) {
			pf.Blockers = append(pf.Blockers, fmt.Sprintf("缺少主分支 workflow %d", wf.ID))
		}
	}
	if info.DefaultBranch != "" && policy.Repository.MainBranch != "" &&
		info.DefaultBranch != policy.Repository.MainBranch {
		pf.Blockers = append(pf.Blockers, fmt.Sprintf("默认分支 %s 与授权主分支 %s 不一致", info.DefaultBranch, policy.Repository.MainBranch))
	}
	pf.Ready = len(pf.Blockers) == 0
	return pf, nil
}

func missingWorkflow(info []githubactions.WorkflowRef, id int64) bool {
	for _, w := range info {
		if w.ID == id {
			return false
		}
	}
	return true
}

// Tick advances one delivery cycle by a full side-effect chain. Recovery and
// callers that cannot wait for the scanning loop use it deliberately.
func (m *Manager) Tick(ctx context.Context, id string) error {
	d, err := m.store.Delivery(id)
	if err != nil {
		return err
	}
	select {
	case <-m.ctx.Done():
		return m.ctx.Err()
	default:
	}
	m.drive(d)
	return nil
}

// Drives wake/revision/periodic-armed scan passes on the worker goroutine.
func (m *Manager) loop() {
	defer close(m.done)
	arm := m.cfg.Clock.After(rescanInterval)
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.revs:
		case <-m.wake:
		case <-arm:
			arm = nil
		}
		m.scanPass()
		arm = m.cfg.Clock.After(rescanInterval)
	}
}

func (m *Manager) scanPass() {
	list, err := m.store.ActiveDeliveries()
	if err != nil {
		return
	}
	for _, d := range list {
		if m.ctx.Err() != nil {
			return
		}
		m.drive(d)
	}
}

// step runs the next side-effect chain of one delivery. cont=true means a
// stage was persisted and the chain should continue (paced).
func (m *Manager) step(d workbench.Delivery) (out workbench.Delivery, cont bool) {
	out = d
	if !sideEffectPhase(d.Phase) {
		// awaiting_acceptance/blocked/paused/needs_check are passive; the
		// explicit user commands and recovery own them.
		return d, false
	}
	if code, reason := m.gateCode(d); code != "" {
		return m.block(d, code, reason)
	}
	// A blocking question keeps the same cycle alive: no side effect runs and
	// nothing is released as done. Malformed interaction output and a missing
	// dependency round still fail closed.
	if code, reason := m.interactionWait(d); code != "" {
		switch code {
		case "interaction_pending", "interaction_unanswered", "interaction_cycle", "interaction_answering":
			return m.waitForUser(d, code, reason)
		default:
			return m.block(d, code, reason)
		}
	}
	if d.WaitingCode != "" {
		next := d
		// Closing a user-answer wait settles its excluded window before the
		// cycle continues; a running user turn ends the segment at its start.
		closeUserWait(&next, m.userWaitEnd(d, m.now()))
		next.WaitingCode, next.WaitingReason = "", ""
		cleared, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
		if err != nil {
			return d, false
		}
		// Re-enter the chain with the cleared cycle; the next pass runs the
		// real stage on the same delivery row.
		return cleared, true
	}
	switch d.Phase {
	case "queued":
		return m.stageQueued(d)
	case "developing":
		return m.stageDeveloping(d)
	case "local_checking":
		return m.stageLocalChecking(d)
	case "committing":
		return m.stageCommitting(d)
	case "pushing_task":
		return m.stagePushingTask(d)
	case "task_ci":
		return m.stageTaskCI(d)
	case "repairing":
		return m.stageRepairing(d)
	case "preparing_integration":
		return m.stagePreparingIntegration(d)
	case "integrating":
		return m.stageIntegrating(d)
	case "integration_checking":
		return m.stageIntegrationChecking(d)
	case "pushing_main":
		return m.stagePushingMain(d)
	case "main_ci":
		return m.stageMainCI(d)
	default:
		// awaiting_acceptance/blocked/paused are passive here; they advance
		// on user acceptance or the next scheduled pass.
		return d, false
	}
}

func sideEffectPhase(phase string) bool {
	switch phase {
	case "queued", "developing", "local_checking", "committing", "pushing_task", "task_ci", "repairing",
		"preparing_integration", "integrating", "integration_checking", "pushing_main", "main_ci":
		return true
	}
	return false
}

// interactionWait reports why dependent automated work must wait for the user.
// During a repair the cycle waits on the repair job's own round, not only the
// cycle's original start round.
func (m *Manager) interactionWait(d workbench.Delivery) (code, reason string) {
	st := m.store.Snapshot()
	roundID := d.RoundID
	if d.Phase == "repairing" {
		if job, err := m.store.LatestDeliveryRepair(d.ID); err == nil && job.MessageID != "" {
			roundID = job.MessageID
		}
	}
	gateCode, gateReason := workbench.InteractionGate(st, d.TaskID, roundID)
	_, _, depCode, depReason := workbench.DeliveryDependency(st, d.TaskID, roundID)
	// Fatal interaction states stay fail-closed even while a turn runs.
	if gateCode == "interaction_invalid" || gateCode == "round_missing" {
		return gateCode, gateReason
	}
	if depCode == "interaction_invalid" || depCode == "round_missing" {
		return depCode, depReason
	}
	if gateCode == "" && depCode == "" {
		return "", ""
	}
	if t, ok := st.Tasks[d.TaskID]; ok {
		// A live native dialog blocks pi in the middle of a running round.
		// That is the user thinking, not AI activity: keep the wait open.
		if workbench.HasPendingNativeInteraction(st, t.ConversationID) {
			return "interaction_pending", "原生请求正在等待你的回答"
		}
		// With an ordinary interaction dependency, a running/starting
		// conversation is real AI work, not user waiting. The hook timestamp
		// makes the wait end deterministic even if the worker only observes the
		// finished round later.
		if c, ok := st.Conversations[t.ConversationID]; ok && (c.RunState == "running" || c.RunState == "starting") {
			return "interaction_answering", "新的用户轮次正在运行，等待其完成"
		}
	}
	if gateCode != "" {
		return gateCode, gateReason
	}
	return depCode, depReason
}

// NoteUserTurn records when a user turn starts a new AI run. It is called by
// the store after the command lock is released and never calls back.
func (m *Manager) NoteUserTurn(taskID, conversationID string) {
	if taskID == "" {
		return
	}
	now := m.now()
	m.mu.Lock()
	if m.userTurnAt == nil {
		m.userTurnAt = map[string]time.Time{}
	}
	m.userTurnAt[taskID] = now
	m.mu.Unlock()
}

// userWaitEnd resolves the exact end of a user-wait segment: the latest user
// turn start that falls inside the segment, otherwise the observation time
// (restart/legacy fallback).
func (m *Manager) userWaitEnd(d workbench.Delivery, now time.Time) time.Time {
	at, ok := m.takeUserTurn(d.TaskID)
	if ok && !at.Before(d.WaitingSince) && !at.After(now) {
		return at
	}
	return now
}

func (m *Manager) takeUserTurn(taskID string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.userTurnAt[taskID]
	if ok {
		delete(m.userTurnAt, taskID)
	}
	return at, ok
}

// waitForUser parks the cycle without changing its phase or dropping its
// frozen policy/candidate evidence. The task write lease is released so the
// user's answer (a real agent turn) can run; the stage re-acquires it after
// the dependency round settles and re-validates the candidate.
//
// Waiting time is accounted explicitly: interaction_pending/unanswered/cycle
// are user waits and are excluded from the activation budget, while
// interaction_answering means the answer round is actually running and must
// count as AI-active time.
func (m *Manager) waitForUser(d workbench.Delivery, code, reason string) (workbench.Delivery, bool) {
	now := m.now()
	next := d
	if userWaitCode(code) {
		// A native response (or a user turn that leaves a question pending)
		// resumed AI work while the wait code stayed open. Split the segment at
		// that exact resume moment, then measure the new wait from this
		// observation: a late worker observation counts the gap as AI time,
		// which is conservative and can never leak budget.
		if at, resumed := m.takeUserTurn(d.TaskID); resumed && !at.Before(d.WaitingSince) && !at.After(now) {
			closeUserWait(&next, at)
			next.WaitingSince = now
		} else if next.WaitingSince.IsZero() {
			next.WaitingSince = now
		}
	} else {
		closeUserWait(&next, m.userWaitEnd(d, now))
	}
	next.WaitingCode, next.WaitingReason = code, reason
	if next.WaitingCode == d.WaitingCode && next.WaitingReason == d.WaitingReason &&
		next.WaitingSince.Equal(d.WaitingSince) && next.WaitExcludedMillis == d.WaitExcludedMillis {
		return d, false
	}
	m.dropLease(d.ID, true)
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	return out, false
}

// userWaitCode separates "a human still has to answer" from "the answer round
// is running". Only the former may be excluded from the activation budget.
func userWaitCode(code string) bool {
	switch code {
	case "interaction_pending", "interaction_unanswered", "interaction_cycle":
		return true
	}
	return false
}

// closeUserWait folds the current user-wait segment into the excluded budget.
func closeUserWait(d *workbench.Delivery, at time.Time) {
	if d.WaitingSince.IsZero() {
		return
	}
	if delta := at.Sub(d.WaitingSince); delta > 0 {
		d.WaitExcludedMillis += delta.Milliseconds()
	}
	d.WaitingSince = time.Time{}
}

// activeElapsed is the AI-active duration of one cycle: total wall time since
// the frozen ActiveSince minus completed and in-flight user waits.
func activeElapsed(d workbench.Delivery, now time.Time) time.Duration {
	if d.ActiveSince.IsZero() {
		return 0
	}
	elapsed := now.Sub(d.ActiveSince) - time.Duration(d.WaitExcludedMillis)*time.Millisecond
	if userWaitCode(d.WaitingCode) && !d.WaitingSince.IsZero() {
		elapsed -= now.Sub(d.WaitingSince)
	}
	return elapsed
}

// drive chains paced stage transitions of one delivery until a passive
// state, a conflict or the end of the pass.
func (m *Manager) drive(d workbench.Delivery) {
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		if !workbench.DeliveryActive(d.Phase) {
			return
		}
		next, cont := m.step(d)
		if !cont {
			return
		}
		time.Sleep(stagePace)
		fresh, err := m.store.Delivery(d.ID)
		if err != nil || fresh.Version != next.Version {
			// Another writer moved the cycle; re-evaluate from the store.
			return
		}
		if !workbench.DeliveryActive(fresh.Phase) {
			return
		}
		d = fresh
	}
}

// block records a terminal blocked state and releases the write lease as
// unsafe. A failed CAS means the cycle moved underneath us: the next pass
// re-evaluates the fresh state instead of forcing a lost write.
func (m *Manager) block(d workbench.Delivery, code, reason string) (workbench.Delivery, bool) {
	m.dropLease(d.ID, false)
	next := d
	next.Phase = "blocked"
	next.BlockCode = code
	next.BlockReason = reason
	out, err := m.store.AdvanceDelivery(d.ID, d.Version, next, nil)
	if err != nil {
		return d, false
	}
	return out, false
}

func (m *Manager) dropLease(deliveryID string, safe bool) {
	m.mu.Lock()
	l := m.leases[deliveryID]
	delete(m.leases, deliveryID)
	m.mu.Unlock()
	if l != nil {
		_ = l.Release(safe)
	}
}

// holdLease stores a freshly acquired lease; releases stay explicit.
func (m *Manager) holdLease(deliveryID string, l agent.DeliveryLease) {
	m.mu.Lock()
	m.leases[deliveryID] = l
	m.mu.Unlock()
}

// leaseFor returns the held lease of one delivery, if any.
func (m *Manager) leaseFor(deliveryID string) agent.DeliveryLease {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leases[deliveryID]
}

// rememberDispatched guards the in-memory intent so cycles never re-send
// within one process; the agent port dedupes by message ID across restarts.
func (m *Manager) rememberDispatched(deliveryID string) {
	m.mu.Lock()
	m.dispatched[deliveryID] = true
	m.mu.Unlock()
}

// dispatchedBefore reports whether this process already dispatched a cycle.
func (m *Manager) dispatchedBefore(deliveryID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dispatched[deliveryID]
}

// ciSignature identifies one CI decision set for change-only persistence.
func ciSignature(runs []workbench.DeliveryCI) string {
	h := make([]string, 0, len(runs))
	for _, r := range runs {
		h = append(h, fmt.Sprintf("%d:%d:%d:%s:%s", r.WorkflowID, r.RunID, r.Attempt, r.Status, r.Conclusion))
	}
	return strings.Join(h, "|")
}

// rememberCI records the last persisted CI signature so 304-equivalent
// waiting observations never rewrite the cycle version.
func (m *Manager) rememberCI(deliveryID, sig string) {
	m.mu.Lock()
	m.lastCI[deliveryID] = sig
	m.mu.Unlock()
}

func (m *Manager) now() time.Time { return m.cfg.Clock.Now() }

func stepID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Randomness failing mid-schedule is fatal enough to surface.
		return fmt.Sprintf("step-fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RemoteTarget builds the frozen remote identity from the authorized policy
// snapshot. Repository configuration is re-verified against the local
// project in later integration stages; here the identity stays policy-only.
func remoteTarget(p workbench.ProjectDeliveryPolicy, taskID string) gitops.RemoteTarget {
	return gitops.RemoteTarget{
		RepositoryID: p.Repository.ID,
		URL:          p.Repository.URL,
		MainRef:      p.Repository.MainBranch,
		TaskRef:      "refs/heads/kaiwu/task-" + taskID,
	}
}
