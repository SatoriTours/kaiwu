package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Subagent runs are recorded by the controlled runtime through the internal Go
// API only. The browser has read access to the bounded summary projection and
// to a paginated detail view; it can never create or mutate a run.

const (
	SubagentStatusStarting    = "starting"
	SubagentStatusRunning     = "running"
	SubagentStatusCompleted   = "completed"
	SubagentStatusFailed      = "failed"
	SubagentStatusInterrupted = "interrupted"
	SubagentStatusUnknown     = "unknown"

	SubagentEventText       = "text"
	SubagentEventToolStart  = "tool_start"
	SubagentEventToolUpdate = "tool_update"
	SubagentEventToolEnd    = "tool_end"
	SubagentEventStatus     = "status"

	MaxSubagentTitleRunes          = 160
	MaxSubagentSummaryRunes        = 400
	MaxSubagentToolIDRunes         = 100
	MaxSubagentToolNameRunes       = 200
	MaxSubagentRequestBytes        = 32 << 10
	MaxSubagentEventContentBytes   = 16 << 10
	MaxSubagentRunContentBytes     = 1 << 20
	MaxSubagentEvents              = 2000
	MaxSubagentRunsPerConversation = 1000
	MaxSubagentRuns                = 10000
	DefaultSubagentDetailLimit     = 50
	MaxSubagentDetailLimit         = 100

	// maxSubagentOverflowFingerprints bounds the accepted status-event
	// fingerprints retained after the event table reached its hard cap, so an
	// exact replay of a terminal transition stays idempotent without keeping an
	// unbounded duplicate log. A run has at most two status transitions, so this
	// is headroom, never a growing store.
	maxSubagentOverflowFingerprints = 4
)

var subagentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`)

// SubagentSummary is the lightweight, browser-visible projection of one run.
// It carries no request body, tool payload, session path or credential.
type SubagentSummary struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"projectId"`
	ConversationID  string  `json:"conversationId"`
	TaskID          string  `json:"taskId,omitempty"`
	RoundID         string  `json:"roundId"`
	ParentSessionID string  `json:"parentSessionId"`
	Title           string  `json:"title"`
	TaskSummary     string  `json:"taskSummary"`
	Status          string  `json:"status"`
	Profile         Profile `json:"profile"`
	StartedAt       string  `json:"startedAt"`
	UpdatedAt       string  `json:"updatedAt"`
	FinishedAt      string  `json:"finishedAt,omitempty"`
	Version         uint64  `json:"version"`
	Truncated       bool    `json:"truncated"`
}

// SubagentEvent is one bounded step of a run's visible execution record.
// Sequence and At are assigned by the server; the remaining fields are a
// fingerprint of the caller's content for event-id idempotency.
type SubagentEvent struct {
	ID       string `json:"id"`
	Sequence uint64 `json:"sequence"`
	At       string `json:"at"`
	Kind     string `json:"kind"`
	Text     string `json:"text,omitempty"`
	ToolID   string `json:"toolId,omitempty"`
	ToolName string `json:"toolName,omitempty"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
	Status   string `json:"status,omitempty"`
}

// SubagentDetailPage is the read-only detail response. Next is the consumed
// sequence cursor; after it, paging is monotonic and never replays an event.
type SubagentDetailPage struct {
	Run     SubagentSummary `json:"run"`
	Request string          `json:"request"`
	Events  []SubagentEvent `json:"events"`
	Next    uint64          `json:"next"`
	HasMore bool            `json:"hasMore"`
}

// subagentRecord is the per-run private row: the full task text, the create
// fingerprint for idempotency, and the accumulated content byte budget. It is
// never part of the browser snapshot.
type subagentRecord struct {
	Fingerprint  string `json:"fingerprint"`
	Request      string `json:"request"`
	ContentBytes int    `json:"contentBytes"`
	// Overflow only holds status events that were applied to the summary after
	// the stored event count reached its hard cap: their rows are never written,
	// but their caller fingerprint is kept so an exact replay is idempotent.
	Overflow []subagentOverflowEvent `json:"overflow,omitempty"`
}

// subagentOverflowEvent is one bounded fingerprint of an accepted status event
// that could not be stored as an event row.
type subagentOverflowEvent struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

func rememberSubagentOverflow(current []subagentOverflowEvent, id, fingerprint string) []subagentOverflowEvent {
	for i := range current {
		if current[i].ID == id {
			current[i].Fingerprint = fingerprint
			return current
		}
	}
	current = append(current, subagentOverflowEvent{ID: id, Fingerprint: fingerprint})
	if len(current) > maxSubagentOverflowFingerprints {
		current = current[len(current)-maxSubagentOverflowFingerprints:]
	}
	return current
}

func validSubagentStatus(status string) bool {
	switch status {
	case SubagentStatusStarting, SubagentStatusRunning, SubagentStatusCompleted, SubagentStatusFailed, SubagentStatusInterrupted, SubagentStatusUnknown:
		return true
	}
	return false
}

func validSubagentEventKind(kind string) bool {
	switch kind {
	case SubagentEventText, SubagentEventToolStart, SubagentEventToolUpdate, SubagentEventToolEnd, SubagentEventStatus:
		return true
	}
	return false
}

func isSubagentTerminal(status string) bool {
	switch status {
	case SubagentStatusCompleted, SubagentStatusFailed, SubagentStatusInterrupted, SubagentStatusUnknown:
		return true
	}
	return false
}

// subagentTransitionAllowed encodes the only legal status edges. Terminal
// states never revive, and unknown never returns to running.
func subagentTransitionAllowed(from, to string) bool {
	switch from {
	case SubagentStatusStarting:
		return to == SubagentStatusRunning || to == SubagentStatusFailed || to == SubagentStatusInterrupted || to == SubagentStatusUnknown
	case SubagentStatusRunning:
		return to == SubagentStatusCompleted || to == SubagentStatusFailed || to == SubagentStatusInterrupted || to == SubagentStatusUnknown
	}
	return false
}

func validSubagentText(text string, max int) bool {
	return utf8.ValidString(text) && strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= max && !strings.ContainsRune(text, 0)
}

func validSubagentOptionalText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

// truncateUTF8Bytes keeps at most max bytes without splitting a rune.
func truncateUTF8Bytes(text string, max int) (string, bool) {
	if max <= 0 {
		return "", text != ""
	}
	if len(text) <= max {
		return text, false
	}
	sliced := text[:max]
	for len(sliced) > 0 && !utf8.ValidString(sliced) {
		sliced = sliced[:len(sliced)-1]
	}
	return sliced, true
}

func truncateRunes(text string, max int) (string, bool) {
	if utf8.RuneCountInString(text) <= max {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:max]), true
}

// normalizeSubagentSummary validates the parent identity, enums, sizes and the
// profile snapshot, and resolves the optional task binding from the
// conversation so that an explicit and a derived task compare identically.
func normalizeSubagentSummary(st State, in SubagentSummary) (SubagentSummary, error) {
	if !subagentIDPattern.MatchString(in.ID) {
		return SubagentSummary{}, Invalid("子 Agent 运行标识无效")
	}
	if in.ProjectID == "" || in.ConversationID == "" {
		return SubagentSummary{}, Missing()
	}
	project, ok := st.Projects[in.ProjectID]
	if !ok {
		return SubagentSummary{}, Missing()
	}
	conversation, ok := st.Conversations[in.ConversationID]
	if !ok || conversation.ProjectID != project.ID {
		return SubagentSummary{}, Missing()
	}
	taskID := in.TaskID
	if taskID == "" {
		taskID = conversation.TaskID
	}
	if taskID != "" {
		task, ok := st.Tasks[taskID]
		if !ok || task.ProjectID != project.ID || conversation.TaskID != taskID {
			return SubagentSummary{}, Missing()
		}
	}
	if in.ParentSessionID == "" || in.ParentSessionID != conversation.SessionID {
		return SubagentSummary{}, Missing()
	}
	if in.RoundID == "" {
		return SubagentSummary{}, Missing()
	}
	roundFound := false
	for _, round := range conversation.Rounds {
		if round.ID == in.RoundID {
			roundFound = true
			break
		}
	}
	if !roundFound {
		return SubagentSummary{}, Missing()
	}
	switch in.Status {
	case SubagentStatusStarting, SubagentStatusRunning:
	default:
		return SubagentSummary{}, Invalid("子 Agent 初始状态只能是 starting 或 running")
	}
	if !validSubagentText(in.Title, MaxSubagentTitleRunes) {
		return SubagentSummary{}, Invalid("子 Agent 标题需为 1–160 字")
	}
	if !validSubagentText(in.TaskSummary, MaxSubagentSummaryRunes) {
		return SubagentSummary{}, Invalid("子 Agent 任务摘要需为 1–400 字")
	}
	provider := strings.TrimSpace(in.Profile.Provider)
	model := strings.TrimSpace(in.Profile.Model)
	if provider == "" || utf8.RuneCountInString(provider) > 200 || model == "" || utf8.RuneCountInString(model) > 300 {
		return SubagentSummary{}, Invalid("子 Agent 模型快照无效")
	}
	switch in.Profile.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return SubagentSummary{}, Invalid("子 Agent 思考级别无效")
	}
	in.ID = strings.TrimSpace(in.ID)
	in.Title = strings.TrimSpace(in.Title)
	in.TaskSummary = strings.TrimSpace(in.TaskSummary)
	in.TaskID = taskID
	in.Profile = Profile{Provider: provider, Model: model, Thinking: in.Profile.Thinking}
	return in, nil
}

// subagentCreateFingerprint covers only caller-supplied content plus the
// resolved task binding; server-assigned timestamps and versions are excluded
// so a retried create stays idempotent.
func subagentCreateFingerprint(summary SubagentSummary, request string) string {
	body, _ := json.Marshal(struct {
		ID              string  `json:"id"`
		ProjectID       string  `json:"projectId"`
		ConversationID  string  `json:"conversationId"`
		TaskID          string  `json:"taskId"`
		RoundID         string  `json:"roundId"`
		ParentSessionID string  `json:"parentSessionId"`
		Title           string  `json:"title"`
		TaskSummary     string  `json:"taskSummary"`
		Status          string  `json:"status"`
		Profile         Profile `json:"profile"`
		Request         string  `json:"request"`
	}{summary.ID, summary.ProjectID, summary.ConversationID, summary.TaskID, summary.RoundID, summary.ParentSessionID, summary.Title, summary.TaskSummary, summary.Status, summary.Profile, request})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// subagentEventFingerprint ignores the server-assigned sequence and time.
func subagentEventFingerprint(event SubagentEvent) string {
	body, _ := json.Marshal(struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Text     string `json:"text"`
		ToolID   string `json:"toolId"`
		ToolName string `json:"toolName"`
		Input    string `json:"input"`
		Output   string `json:"output"`
		Failed   bool   `json:"failed"`
		Status   string `json:"status"`
	}{event.ID, event.Kind, event.Text, event.ToolID, event.ToolName, event.Input, event.Output, event.Failed, event.Status})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
