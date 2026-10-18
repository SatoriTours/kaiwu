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
}
type MergeRequest struct {
	OperationID   string `json:"operationId"`
	ProjectID     string `json:"projectId"`
	TaskID        string `json:"taskId"`
	PreviewID     string `json:"previewId"`
	CommitMessage string `json:"commitMessage"`
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
	Error         string       `json:"error,omitempty"`
	StartedAt     string       `json:"startedAt"`
	FinishedAt    string       `json:"finishedAt,omitempty"`
	Preview       MergePreview `json:"preview"`
}

func MergeActive(phase string) bool {
	switch phase {
	case "queued", "preparing", "committing", "committed", "merging":
		return true
	}
	return false
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
	if conv.RunState == "running" || conv.RunState == "starting" || TaskMerging(s.state, task.ID) {
		return MergeOperation{}, false, Conflict("任务执行或合并中，请稍后重试")
	}
	// A running delivery cycle owns the integration lane; the old manual merge
	// path must not race it for the same main branch.
	if summary, ok := s.state.Deliveries[task.ID]; ok && DeliveryActive(summary.Phase) {
		return MergeOperation{}, false, Conflict("任务交付进行中，请在交付流程内完成或取消后再手动合并")
	}
	if !p.CanMerge || p.ID != req.PreviewID || p.ProjectID != req.ProjectID || p.TaskID != req.TaskID {
		return MergeOperation{}, false, Conflict("合并预检无效，请重新检查")
	}
	op := MergeOperation{ID: req.OperationID, ProjectID: req.ProjectID, TaskID: req.TaskID, PreviewID: req.PreviewID, CommitMessage: strings.TrimSpace(req.CommitMessage), Phase: "queued", TaskBeforeSHA: p.TaskSHA, MainBeforeSHA: p.MainSHA, Fingerprint: p.Fingerprint, Preview: p, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	next := clone(s.state)
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
