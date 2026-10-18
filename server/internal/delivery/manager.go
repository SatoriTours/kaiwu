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
	}
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
	switch d.Phase {
	case "queued", "developing", "local_checking", "committing", "pushing_task", "task_ci", "repairing",
		"preparing_integration", "integrating", "integration_checking", "pushing_main", "main_ci":
		if code, reason := m.gateCode(d); code != "" {
			return m.block(d, code, reason)
		}
	default:
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
