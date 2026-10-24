package workbench

import (
	"errors"
	"strings"
	"time"
)

// PushReviewFreeze is everything the server authorised for one manual push. It
// is persisted with the queued operation before any commit, merge or push, so a
// crash, timeout or lost receipt can never widen the authorisation and recovery
// can compare against exactly these values instead of the current repository
// state.
//
// It contains no credential: only the resolver reference name.
type PushReviewFreeze struct {
	Fingerprint       string        `json:"fingerprint"`
	ReviewFingerprint string        `json:"reviewFingerprint"`
	RemoteName        string        `json:"remoteName"`
	RemoteURL         string        `json:"remoteUrl"`
	MainRef           string        `json:"mainRef"`
	CredentialRef     string        `json:"credentialRef,omitempty"`
	ObservedSHA       string        `json:"observedSHA"`
	LocalMainSHA      string        `json:"localMainSHA"`
	TaskSHA           string        `json:"taskSHA"`
	Uploads           []MergeUpload `json:"uploads"`
	Pending           []GitFile     `json:"pending"`
	PendingNote       string        `json:"pendingNote,omitempty"`
	SafetyProof       string        `json:"safetyProof,omitempty"`
	RoundID           string        `json:"roundId"`
	// ContentProof marks a fingerprint derived from committed content rather
	// than from a live preview, which is what an already merged task yields.
	ContentProof bool `json:"contentProof,omitempty"`
	// PushOnly freezes the fact that main already carries the reviewed content,
	// so the executor must not create another commit or merge locally.
	PushOnly bool `json:"pushOnly,omitempty"`
}

// FreezeFromReview turns an accepted review into the immutable authorisation.
func FreezeFromReview(r MergeReview) PushReviewFreeze {
	return PushReviewFreeze{
		Fingerprint:       r.Fingerprint,
		ReviewFingerprint: r.ReviewFingerprint,
		RemoteName:        r.RemoteName,
		RemoteURL:         r.RemoteURL,
		MainRef:           r.MainRef,
		CredentialRef:     r.CredentialRef,
		ObservedSHA:       r.ObservedRemoteSHA,
		LocalMainSHA:      r.LocalMainSHA,
		TaskSHA:           r.TaskSHA,
		Uploads:           append([]MergeUpload{}, r.Uploads...),
		Pending:           append([]GitFile{}, r.Pending...),
		PendingNote:       r.PendingNote,
		SafetyProof:       r.SafetyProof,
		RoundID:           r.RoundID,
		ContentProof:      !r.CanMerge,
		PushOnly:          r.PushOnly,
	}
}

// Freeze returns the frozen authorisation carried by a queued operation.
func (o MergeOperation) Freeze() PushReviewFreeze {
	if o.Push == nil {
		return PushReviewFreeze{}
	}
	return *o.Push
}

// PendingPaths is the exact set of files the reviewed uncommitted work may
// touch. It bounds any commit this operation is allowed to create.
func (f PushReviewFreeze) PendingPaths() map[string]bool {
	out := map[string]bool{}
	for _, v := range f.Pending {
		out[v.Path] = true
	}
	return out
}

// UploadSHAs is the reviewed upload set, used to prove the pushed result is
// exactly the reviewed history plus at most one new task commit.
func (f PushReviewFreeze) UploadSHAs() map[string]bool {
	out := map[string]bool{}
	for _, v := range f.Uploads {
		out[v.SHA] = true
	}
	return out
}

// BeginMergeReview admits a reviewed push. It freezes the whole remote review
// into the operation and applies the result acknowledgement in the same persist,
// so admission is all-or-nothing. Everything the operation may later touch comes
// from the frozen review; the client only supplies identifiers.
func (s *Store) BeginMergeReview(req MergeRequest, r MergeReview) (MergeOperation, bool, error) {
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
	if summary, ok := s.state.Deliveries[task.ID]; ok && DeliveryActive(summary.Phase) {
		return MergeOperation{}, false, Conflict("任务交付进行中，请在交付流程内完成或取消后再手动合并")
	}
	if req.Mode != MergeModePush {
		return MergeOperation{}, false, Invalid("未知的合并模式")
	}
	if !r.CanPush {
		return MergeOperation{}, false, Conflict("远程审核未通过，不能推送")
	}
	if r.ID != req.PreviewID || r.ProjectID != req.ProjectID || r.TaskID != req.TaskID {
		return MergeOperation{}, false, Conflict("审核凭据与请求不一致，请重新审核")
	}
	if r.Mode != req.Mode || r.RoundID != req.RoundID {
		return MergeOperation{}, false, Conflict("审核预检与请求的模式或轮次不一致，请重新审核")
	}
	if r.ReviewFingerprint == "" || r.RemoteURL == "" || r.MainRef == "" || r.LocalMainSHA == "" || r.TaskSHA == "" {
		return MergeOperation{}, false, Conflict("审核凭据不完整，请重新审核")
	}
	expire, e := time.Parse(time.RFC3339, r.ExpiresAt)
	if e != nil || time.Now().After(expire) {
		return MergeOperation{}, false, Conflict("预检已过期，请重新审核")
	}
	if _, e := ReviewedTask(s.state, req); e != nil {
		return MergeOperation{}, false, e
	}
	op := MergeOperation{
		ID: req.OperationID, ProjectID: req.ProjectID, TaskID: req.TaskID, PreviewID: req.PreviewID,
		CommitMessage: strings.TrimSpace(req.CommitMessage), Phase: "queued",
		TaskBeforeSHA: r.TaskSHA, MainBeforeSHA: r.LocalMainSHA,
		Fingerprint: r.ReviewFingerprint, Mode: req.Mode, RoundID: req.RoundID,
		Preview: r.MergePreview, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	freeze := FreezeFromReview(r)
	op.Push = &freeze
	next := clone(s.state)
	if _, e := acknowledgeReviewedRound(&next, req); e != nil {
		return MergeOperation{}, false, e
	}
	next.Merges[op.ID] = op
	e = s.persist(next, op.ID, mergeFingerprint(req), Receipt{Type: "merge_task", CreatedID: op.ID})
	return op, false, e
}

// PendingFiles is the concrete uncommitted set a commit would capture.
func PendingFiles(p MergePreview) []GitFile {
	if !p.NeedsCommit {
		return nil
	}
	return append([]GitFile{}, p.Files...)
}
