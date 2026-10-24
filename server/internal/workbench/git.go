package workbench

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

type GitFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}
type GitStatus struct {
	TaskID     string    `json:"taskId"`
	State      string    `json:"state"`
	MainBranch string    `json:"mainBranch"`
	MainSHA    string    `json:"mainSHA"`
	TaskSHA    string    `json:"taskSHA"`
	Reason     string    `json:"reason,omitempty"`
	CheckedAt  string    `json:"checkedAt"`
	DirtyFiles []GitFile `json:"dirtyFiles"`
	CanMerge   bool      `json:"canMerge"`
}

// MergeModeLocal is the only mode this slice can execute: an in-process local
// integration. MergeModePush is reserved and currently refused before any
// remote capability is touched.
const (
	MergeModeLocal = "local"
	MergeModePush  = "push"
)

type MergePreview struct {
	Blobs       map[string]string `json:"blobs,omitempty"`
	Modes       map[string]string `json:"modes,omitempty"`
	ID          string            `json:"id"`
	ProjectID   string            `json:"projectId"`
	TaskID      string            `json:"taskId"`
	MainBranch  string            `json:"mainBranch"`
	MainSHA     string            `json:"mainSHA"`
	TaskSHA     string            `json:"taskSHA"`
	Fingerprint string            `json:"fingerprint"`
	CommitName  string            `json:"commitName"`
	CommitEmail string            `json:"commitEmail"`
	Reason      string            `json:"reason,omitempty"`
	ExpiresAt   string            `json:"expiresAt"`
	Files       []GitFile         `json:"files"`
	NeedsCommit bool              `json:"needsCommit"`
	CanMerge    bool              `json:"canMerge"`
	// Mode and RoundID appear only for a reviewed (non-legacy) request, so the
	// legacy request JSON and its fingerprint stay byte-identical.
	Mode    string `json:"mode,omitempty"`
	RoundID string `json:"roundId,omitempty"`
}

// MergeUpload is one commit a push review would upload, newest first.
type MergeUpload struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// MergeReview is the read-only manual review of a task against its configured
// remote. It always carries the full local proof (embedded MergePreview) plus
// the observed remote state and the exact upload set, so an already-merged task
// still yields a complete, verifiable proof instead of a bare "cannot merge".
type MergeReview struct {
	MergePreview
	RemoteName        string        `json:"remoteName,omitempty"`
	RemoteURL         string        `json:"remoteUrl,omitempty"`
	MainRef           string        `json:"mainRef,omitempty"`
	ObservedRemoteSHA string        `json:"observedRemoteSHA"`
	LocalMainSHA      string        `json:"localMainSHA,omitempty"`
	Uploads           []MergeUpload `json:"uploads"`
	Pending           []GitFile     `json:"pending"`
	AlreadyMerged     bool          `json:"alreadyMerged"`
	PushOnly          bool          `json:"pushOnly"`
	CanPush           bool          `json:"canPush"`
	PushReason        string        `json:"pushReason,omitempty"`
	// SafetyProof records which local admission evidence the review verified, so
	// an already-merged task still carries a complete proof.
	SafetyProof string `json:"safetyProof,omitempty"`
	// PendingNote states explicitly that uncommitted work is not part of the
	// upload set yet, so a review never implies a commit already exists.
	PendingNote string `json:"pendingNote,omitempty"`
	// CredentialRef names the server resolver reference, never a secret.
	CredentialRef     string `json:"credentialRef,omitempty"`
	ReviewFingerprint string `json:"reviewFingerprint"`
}

// MergeReviewRequest is the internal reviewed-merge entry: a local review is
// bound to the newest finished round of the task's conversation.
type MergeReviewRequest struct {
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
	Mode      string `json:"mode"`
	RoundID   string `json:"roundId"`
}

type MergeRequest struct {
	OperationID   string `json:"operationId"`
	ProjectID     string `json:"projectId"`
	TaskID        string `json:"taskId"`
	PreviewID     string `json:"previewId"`
	CommitMessage string `json:"commitMessage"`
	// Mode empty means the legacy path: no acknowledgement, no round binding.
	Mode    string `json:"mode,omitempty"`
	RoundID string `json:"roundId,omitempty"`
}
type MergeOperation struct {
	ID            string       `json:"id"`
	ProjectID     string       `json:"projectId"`
	TaskID        string       `json:"taskId"`
	PreviewID     string       `json:"previewId"`
	CommitMessage string       `json:"commitMessage"`
	Phase         string       `json:"phase"`
	TaskBeforeSHA string       `json:"taskBeforeSHA"`
	TaskAfterSHA  string       `json:"taskAfterSHA"`
	MainBeforeSHA string       `json:"mainBeforeSHA"`
	MainAfterSHA  string       `json:"mainAfterSHA"`
	Fingerprint   string       `json:"fingerprint"`
	Mode          string       `json:"mode,omitempty"`
	RoundID       string       `json:"roundId,omitempty"`
	Error         string       `json:"error,omitempty"`
	StartedAt     string       `json:"startedAt"`
	FinishedAt    string       `json:"finishedAt,omitempty"`
	Preview       MergePreview `json:"preview"`
	// Push freezes the authorised remote work; nil for local operations.
	Push *PushReviewFreeze `json:"push,omitempty"`
	// IntendedSHA is the exact 40-hex commit this operation may push. It is
	// persisted before the Git write so a lost receipt is never ambiguous.
	IntendedSHA string `json:"intendedSHA,omitempty"`
	// RemoteAfterSHA is only set once the remote was observed to contain the
	// intended commit. CIStatus stays "not_checked" until CI is actually read.
	RemoteAfterSHA  string `json:"remoteAfterSHA,omitempty"`
	PushAttemptedAt string `json:"pushAttemptedAt,omitempty"`
	CIStatus        string `json:"ciStatus,omitempty"`
}

// MergeActive reports phases whose executor still owns the project/task leases.
// merged and remote_done are included: after a crash between the local merge and
// the terminal write the operation must still block a new writer and stay
// eligible for lease-guarded recovery.
func MergeActive(phase string) bool {
	switch phase {
	case "queued", "preparing", "committing", "committed", "merging", "merged", "pushing", "remote_done":
		return true
	}
	return false
}

// MergeIncomplete lists the non-terminal phases recovery may adopt. It never
// includes a terminal phase, so succeeded/failed/needs_check keep their meaning.
func MergeIncomplete(phase string) bool {
	return MergeActive(phase)
}
func TaskMerging(st State, taskID string) bool {
	if taskID == "" {
		return false
	}
	for _, op := range st.Merges {
		if op.TaskID == taskID && MergeActive(op.Phase) {
			return true
		}
	}
	return false
}
func mergeFingerprint(req MergeRequest) string {
	b, _ := json.Marshal(req)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func (s *Store) findMergeLocked(req MergeRequest) (MergeOperation, bool, error) {
	var fp string
	var receipt []byte
	e := s.db.QueryRow("SELECT fingerprint,receipt FROM operations WHERE id=?", req.OperationID).Scan(&fp, &receipt)
	if errors.Is(e, sql.ErrNoRows) {
		return MergeOperation{}, false, nil
	}
	if e != nil {
		return MergeOperation{}, false, e
	}
	if fp != mergeFingerprint(req) {
		return MergeOperation{}, false, Conflict("操作标识已用于不同内容")
	}
	op, ok := s.state.Merges[req.OperationID]
	if !ok {
		return op, false, Conflict("该标识不是合并操作")
	}
	return op, true, nil
}
func (s *Store) FindMerge(req MergeRequest) (MergeOperation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findMergeLocked(req)
}

// confirmLegacyToken mirrors the exact version token ConfirmResult synthesizes
// for a task that has no recorded Round yet. Keeping the two in lockstep is what
// lets a legacy task pass the same review gate.
func confirmLegacyToken(x Execution) string {
	return "legacy:" + x.ID + ":" + x.FinishedAt
}

// ReviewedTask validates that req names a local review whose newest finished
// round, execution evidence and run state may be acknowledged. It reuses
// ConfirmResult's safety gates (no pending interaction, run finished, latest
// round finished) and additionally accepts an already confirmed round.
func ReviewedTask(st State, req MergeRequest) (Task, error) {
	if req.Mode != MergeModeLocal && req.Mode != MergeModePush {
		return Task{}, Invalid("未知的合并模式")
	}
	t, ok := st.Tasks[req.TaskID]
	if !ok || t.ProjectID != req.ProjectID {
		return Task{}, Missing()
	}
	conv := st.Conversations[t.ConversationID]
	if _, pending := PendingBlockingInteraction(st, conv.ID); pending {
		return Task{}, Conflict("还有待你处理的交互问题，回答或取消后才能合并")
	}
	if conv.RunState != "idle" && conv.RunState != "interrupted" && conv.RunState != "failed" {
		return Task{}, Conflict("任务仍在执行，不能认可或合并")
	}
	x, ok := st.Executions[t.ExecutionID]
	if !ok || x.TaskID != t.ID || x.ConversationID != conv.ID || x.FinishedAt == "" {
		return Task{}, Conflict("没有已完成且可确认的执行结果")
	}
	if req.RoundID == "" {
		return Task{}, Conflict("轮次已变化，请核对最新结果后重新审核")
	}
	if len(conv.Rounds) == 0 {
		// Legacy tasks have no Round slice: the only acceptable evidence is the
		// exact token ConfirmResult itself synthesizes from persisted completion.
		if x.Phase != "awaiting_review" || t.Status != "needs_confirmation" || req.RoundID != confirmLegacyToken(x) {
			return Task{}, Conflict("没有已完成且可确认的结果")
		}
		return t, nil
	}
	r := conv.Rounds[len(conv.Rounds)-1]
	if r.ID != req.RoundID || r.FinishedAt == "" {
		return Task{}, Conflict("轮次已变化，请核对最新结果后重新审核")
	}
	switch r.Phase {
	case "confirmed":
		// Reusing an acknowledgement must not need (or perform) a second write,
		// but it still requires the execution and task to agree with it.
		// A later CLI failure keeps a finished, acknowledged result: the guard
		// above already rejected a live or waiting conversation.
		if x.Phase != "confirmed" || t.Status != "completed" {
			return Task{}, Conflict("轮次认可状态与执行记录不一致，请核查")
		}
	case "finished":
		if x.Phase != "awaiting_review" || t.Status != "needs_confirmation" {
			return Task{}, Conflict("执行结果尚未完成，不能认可")
		}
	default:
		return Task{}, Conflict("轮次尚未结束，不能认可")
	}
	return t, nil
}

// acknowledgeReviewedRound applies the result confirmation to the state that
// carries the queued merge, so both land in one atomic persist.
func acknowledgeReviewedRound(next *State, req MergeRequest) (Receipt, error) {
	t, e := ReviewedTask(*next, req)
	if e != nil {
		return Receipt{}, e
	}
	// ConfirmResult itself appends the synthesized legacy round, so the reuse
	// check must look at the last round only after confirming it exists.
	if rounds := next.Conversations[t.ConversationID].Rounds; len(rounds) > 0 && rounds[len(rounds)-1].Phase == "confirmed" {
		return Receipt{ConversationID: t.ConversationID}, nil
	}
	return ConfirmResult(next, Command{Type: "confirm_result", OperationID: req.OperationID, ProjectID: req.ProjectID, TaskID: req.TaskID, ConversationID: t.ConversationID, RoundID: req.RoundID})
}

func (s *Store) BeginMerge(req MergeRequest, p MergePreview) (MergeOperation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return MergeOperation{}, false, errors.New("store closed")
	}
	if op, ok, e := s.findMergeLocked(req); ok || e != nil {
		return op, ok, e
	}
	if len(req.OperationID) < 3 || len(req.OperationID) > 100 || !validText(req.CommitMessage, 500) || strings.ContainsRune(req.CommitMessage, 0) {
		return MergeOperation{}, false, Invalid("操作标识或提交说明无效")
	}
	task, ok := s.state.Tasks[req.TaskID]
	if !ok || task.ProjectID != req.ProjectID {
		return MergeOperation{}, false, Missing()
	}
	conv := s.state.Conversations[task.ConversationID]
	if _, pending := PendingBlockingInteraction(s.state, conv.ID); pending {
		return MergeOperation{}, false, Conflict("还有待你处理的交互问题，回答或取消后才能合并")
	}
	if conv.RunState == "running" || conv.RunState == "starting" || TaskMerging(s.state, task.ID) {
		return MergeOperation{}, false, Conflict("任务执行或合并中，请稍后重试")
	}
	// A running delivery cycle owns the integration lane; the old manual merge
	// path must not race it for the same main branch.
	if summary, ok := s.state.Deliveries[task.ID]; ok && DeliveryActive(summary.Phase) {
		return MergeOperation{}, false, Conflict("任务交付进行中，请在交付流程内完成或取消后再手动合并")
	}
	// Push is refused before any other check so it can never be reached through
	// a preview that happens to name push mode.
	if req.Mode == MergeModePush {
		return MergeOperation{}, false, Conflict("推送合并尚未开通，本版本只支持本地合并")
	}
	if !p.CanMerge || p.ID != req.PreviewID || p.ProjectID != req.ProjectID || p.TaskID != req.TaskID {
		return MergeOperation{}, false, Conflict("合并预检无效，请重新检查")
	}
	if p.Mode != req.Mode || p.RoundID != req.RoundID {
		return MergeOperation{}, false, Conflict("审核预检与请求的模式或轮次不一致，请重新审核")
	}
	switch req.Mode {
	case "":
		// Legacy manual merge: exactly the previous behaviour, no acknowledgement.
	case MergeModeLocal:
		// Reject anything unconfirmable before the single atomic persist below.
		if _, e := ReviewedTask(s.state, req); e != nil {
			return MergeOperation{}, false, e
		}
	default:
		return MergeOperation{}, false, Invalid("未知的合并模式")
	}
	if req.Mode != "" && !p.CanMerge {
		return MergeOperation{}, false, Conflict("合并预检无效，请重新检查")
	}
	op := MergeOperation{ID: req.OperationID, ProjectID: req.ProjectID, TaskID: req.TaskID, PreviewID: req.PreviewID, CommitMessage: strings.TrimSpace(req.CommitMessage), Phase: "queued", TaskBeforeSHA: p.TaskSHA, MainBeforeSHA: p.MainSHA, Fingerprint: p.Fingerprint, Mode: req.Mode, RoundID: req.RoundID, Preview: p, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	next := clone(s.state)
	// The acknowledgement is applied to the clone and committed by the same
	// persist call that inserts the queued merge, so a failed admission leaves
	// no half-recorded acknowledgement behind.
	if req.Mode == MergeModeLocal {
		if _, e := acknowledgeReviewedRound(&next, req); e != nil {
			return MergeOperation{}, false, e
		}
	}
	next.Merges[op.ID] = op
	e := s.persist(next, op.ID, mergeFingerprint(req), Receipt{Type: "merge_task", CreatedID: op.ID})
	return op, false, e
}
func (s *Store) AdvanceMerge(id string, mutate func(*MergeOperation) error) error {
	return s.Update(func(st *State) error {
		op, ok := st.Merges[id]
		if !ok {
			return Missing()
		}
		if e := mutate(&op); e != nil {
			return e
		}
		st.Merges[id] = op
		return nil
	})
}
func (s *Store) SaveGitStatus(status GitStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	old := s.state.TaskGit[status.TaskID]
	a, b := old, status
	a.CheckedAt = ""
	b.CheckedAt = ""
	if reflect.DeepEqual(a, b) {
		return nil
	}
	next := clone(s.state)
	next.TaskGit[status.TaskID] = status
	return s.persist(next, "", "", Receipt{})
}
