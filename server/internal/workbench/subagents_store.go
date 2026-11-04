package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CreateSubagentRun records a new run for a trusted runtime. It is never
// reachable from the browser: there is no command type, HTTP route or socket
// method that calls it. The same id with the same normalized content is
// idempotent; the same id with different content is a conflict.
func (s *Store) CreateSubagentRun(summary SubagentSummary, request string) (SubagentSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SubagentSummary{}, errors.New("store closed")
	}
	normalized, err := normalizeSubagentSummary(s.state, summary)
	if err != nil {
		return SubagentSummary{}, err
	}
	if !utf8ValidRequest(request) {
		return SubagentSummary{}, Invalid("子 Agent 任务描述需为 1–32 KiB 的有效文本")
	}
	fingerprint := subagentCreateFingerprint(normalized, request)
	if existing, ok := s.state.Subagents[normalized.ID]; ok {
		record, err := s.subagentRecordLocked(normalized.ID)
		if err != nil {
			return SubagentSummary{}, err
		}
		if record.Fingerprint != fingerprint {
			return SubagentSummary{}, Conflict("子 Agent 运行标识已用于不同内容")
		}
		return existing, nil
	}
	perConversation, total, err := s.subagentRunCountsLocked(normalized.ConversationID)
	if err != nil {
		return SubagentSummary{}, err
	}
	if perConversation >= MaxSubagentRunsPerConversation {
		return SubagentSummary{}, Invalid("该会话的子 Agent 运行记录已达上限，请归档旧记录")
	}
	if total >= MaxSubagentRuns {
		return SubagentSummary{}, Invalid("子 Agent 运行记录已达全库上限，请归档旧记录")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	normalized.StartedAt = now
	normalized.UpdatedAt = now
	normalized.FinishedAt = ""
	normalized.Version = 1
	normalized.Truncated = false
	body, err := json.Marshal(subagentRecord{Fingerprint: fingerprint, Request: request})
	if err != nil {
		return SubagentSummary{}, err
	}
	next := clone(s.state)
	next.Subagents[normalized.ID] = normalized
	err = s.persistWith(next, "", "", Receipt{}, func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO subagent_runs(id,project_id,conversation_id,task_id,round_id,parent_session_id,body) VALUES(?,?,?,?,?,?,?)",
			normalized.ID, normalized.ProjectID, normalized.ConversationID, normalized.TaskID, normalized.RoundID, normalized.ParentSessionID, body)
		return e
	})
	if err != nil {
		return SubagentSummary{}, err
	}
	return normalized, nil
}

// AppendSubagentEvent stores one bounded event of a run. The server assigns the
// sequence and timestamp. Content over the per-event or per-run budget is
// truncated on a UTF-8 boundary and the summary is flagged; status transitions
// and the terminal state are still recorded afterwards. The stored event count
// has a hard cap, beyond which only the status transition survives.
func (s *Store) AppendSubagentEvent(conversationID, runID string, event SubagentEvent) (SubagentSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SubagentSummary{}, errors.New("store closed")
	}
	summary, ok := s.state.Subagents[runID]
	if !ok || summary.ConversationID != conversationID {
		return SubagentSummary{}, Missing()
	}
	if !subagentIDPattern.MatchString(event.ID) {
		return SubagentSummary{}, Invalid("子 Agent 事件标识无效")
	}
	record, err := s.subagentRecordLocked(runID)
	if err != nil {
		return SubagentSummary{}, err
	}
	// Idempotency is decided before any transition or content rule, and it
	// compares the caller's original content. An exact replay of an accepted
	// event therefore stays accepted even when the run already advanced past it,
	// when it is terminal, or when the stored copy was truncated.
	fingerprint := subagentEventFingerprint(event)
	var storedFingerprint string
	err = s.db.QueryRow("SELECT fingerprint FROM subagent_events WHERE run_id=? AND event_id=?", runID, event.ID).Scan(&storedFingerprint)
	if err == nil {
		if storedFingerprint != fingerprint {
			return SubagentSummary{}, Conflict("子 Agent 事件标识已用于不同内容")
		}
		return summary, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SubagentSummary{}, err
	}
	for _, accepted := range record.Overflow {
		if accepted.ID != event.ID {
			continue
		}
		if accepted.Fingerprint != fingerprint {
			return SubagentSummary{}, Conflict("子 Agent 事件标识已用于不同内容")
		}
		return summary, nil
	}
	if !validSubagentEventKind(event.Kind) {
		return SubagentSummary{}, Invalid("子 Agent 事件类型无效")
	}
	if event.Kind == SubagentEventStatus {
		if !validSubagentStatus(event.Status) {
			return SubagentSummary{}, Invalid("子 Agent 状态无效")
		}
		if !subagentTransitionAllowed(summary.Status, event.Status) {
			return SubagentSummary{}, Conflict("子 Agent 状态不能从 " + summary.Status + " 变为 " + event.Status)
		}
	} else if event.Status != "" {
		return SubagentSummary{}, Invalid("只有状态事件可以携带状态")
	}
	for _, text := range []string{event.Text, event.Input, event.Output, event.ToolID, event.ToolName} {
		if !validSubagentOptionalText(text) {
			return SubagentSummary{}, Invalid("子 Agent 事件内容不是有效文本")
		}
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM subagent_events WHERE run_id=?", runID).Scan(&count); err != nil {
		return SubagentSummary{}, err
	}
	at := time.Now().UTC().Format(time.RFC3339)
	if count >= MaxSubagentEvents {
		// The hard cap keeps a fixed number of stored rows. A further status
		// transition is preserved in the summary and keeps its caller
		// fingerprint so it stays idempotent; further content is dropped and
		// flagged, and a dropped event is deliberately not recorded, so no
		// conflict can be claimed for it.
		next := summary
		if event.Kind == SubagentEventStatus {
			next.Status = event.Status
			if isSubagentTerminal(event.Status) {
				next.FinishedAt = at
			}
			record.Overflow = rememberSubagentOverflow(record.Overflow, event.ID, fingerprint)
		} else if summary.Truncated {
			return summary, nil
		}
		next.UpdatedAt = at
		next.Truncated = true
		next.Version++
		state := clone(s.state)
		state.Subagents[runID] = next
		var writeDetail func(*sql.Tx) error
		if event.Kind == SubagentEventStatus {
			recordBody, e := json.Marshal(record)
			if e != nil {
				return SubagentSummary{}, e
			}
			writeDetail = func(tx *sql.Tx) error {
				_, e := tx.Exec("UPDATE subagent_runs SET body=? WHERE id=?", recordBody, runID)
				return e
			}
		}
		if err = s.persistWith(state, "", "", Receipt{}, writeDetail); err != nil {
			return SubagentSummary{}, err
		}
		return next, nil
	}
	sequence, err := s.nextSubagentSequenceLocked(runID)
	if err != nil {
		return SubagentSummary{}, err
	}
	stored := SubagentEvent{ID: event.ID, Sequence: sequence, At: at, Kind: event.Kind, Failed: event.Failed, Status: event.Status}
	var truncated, cut bool
	stored.ToolID, truncated = truncateRunes(event.ToolID, MaxSubagentToolIDRunes)
	stored.ToolName, cut = truncateRunes(event.ToolName, MaxSubagentToolNameRunes)
	truncated = truncated || cut
	budget := MaxSubagentEventContentBytes
	if remaining := MaxSubagentRunContentBytes - record.ContentBytes; remaining < budget {
		budget = remaining
	}
	if budget < 0 {
		budget = 0
	}
	stored.Text, cut = truncateUTF8Bytes(event.Text, budget)
	truncated = truncated || cut
	budget -= len(stored.Text)
	stored.Input, cut = truncateUTF8Bytes(event.Input, budget)
	truncated = truncated || cut
	budget -= len(stored.Input)
	stored.Output, cut = truncateUTF8Bytes(event.Output, budget)
	truncated = truncated || cut
	record.ContentBytes += len(stored.Text) + len(stored.Input) + len(stored.Output)
	next := summary
	next.UpdatedAt = at
	next.Version++
	if truncated {
		next.Truncated = true
	}
	if event.Kind == SubagentEventStatus {
		next.Status = event.Status
		if isSubagentTerminal(event.Status) {
			next.FinishedAt = at
		}
	}
	eventBody, err := json.Marshal(stored)
	if err != nil {
		return SubagentSummary{}, err
	}
	recordBody, err := json.Marshal(record)
	if err != nil {
		return SubagentSummary{}, err
	}
	state := clone(s.state)
	state.Subagents[runID] = next
	err = s.persistWith(state, "", "", Receipt{}, func(tx *sql.Tx) error {
		if _, e := tx.Exec("UPDATE subagent_runs SET body=? WHERE id=?", recordBody, runID); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO subagent_events(run_id,sequence,event_id,fingerprint,body) VALUES(?,?,?,?,?)", runID, sequence, stored.ID, fingerprint, eventBody)
		return e
	})
	if err != nil {
		return SubagentSummary{}, err
	}
	return next, nil
}

// SubagentDetail returns one scoped, read-only page of a run. A run is only
// visible through its own project and conversation; any other scope is refused
// as not found so details can never be strung across tasks.
func (s *Store) SubagentDetail(projectID, conversationID, runID string, after uint64, limit int) (SubagentDetailPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SubagentDetailPage{}, errors.New("store closed")
	}
	if limit < 0 || limit > MaxSubagentDetailLimit {
		return SubagentDetailPage{}, Invalid("详情分页大小需在 1–100 之间")
	}
	if limit == 0 {
		limit = DefaultSubagentDetailLimit
	}
	summary, ok := s.state.Subagents[runID]
	if !ok || summary.ProjectID != projectID || summary.ConversationID != conversationID {
		return SubagentDetailPage{}, Missing()
	}
	record, err := s.subagentRecordLocked(runID)
	if err != nil {
		return SubagentDetailPage{}, err
	}
	rows, err := s.db.Query("SELECT body FROM subagent_events WHERE run_id=? AND sequence>? ORDER BY sequence LIMIT ?", runID, after, limit+1)
	if err != nil {
		return SubagentDetailPage{}, err
	}
	page := SubagentDetailPage{Run: summary, Request: record.Request, Events: []SubagentEvent{}, Next: after}
	for rows.Next() {
		var body []byte
		var event SubagentEvent
		if err = rows.Scan(&body); err != nil {
			_ = rows.Close()
			return SubagentDetailPage{}, err
		}
		if err = json.Unmarshal(body, &event); err != nil {
			_ = rows.Close()
			return SubagentDetailPage{}, err
		}
		if len(page.Events) == limit {
			page.HasMore = true
			break
		}
		page.Events = append(page.Events, event)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return SubagentDetailPage{}, err
	}
	if err = rows.Close(); err != nil {
		return SubagentDetailPage{}, err
	}
	if len(page.Events) > 0 {
		page.Next = page.Events[len(page.Events)-1].Sequence
	}
	return page, nil
}

func utf8ValidRequest(request string) bool {
	return validSubagentOptionalText(request) && strings.TrimSpace(request) != "" && len(request) <= MaxSubagentRequestBytes
}

// The locked helpers below run with Store.mu held and touch SQL only.
func (s *Store) subagentRecordLocked(runID string) (subagentRecord, error) {
	var body []byte
	if err := s.db.QueryRow("SELECT body FROM subagent_runs WHERE id=?", runID).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return subagentRecord{}, errors.New("子 Agent 记录与摘要不一致")
		}
		return subagentRecord{}, err
	}
	var record subagentRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return subagentRecord{}, err
	}
	return record, nil
}

func (s *Store) subagentRunCountsLocked(conversationID string) (int, int, error) {
	var perConversation, total int
	if err := s.db.QueryRow("SELECT count(*) FROM subagent_runs WHERE conversation_id=?", conversationID).Scan(&perConversation); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRow("SELECT count(*) FROM subagent_runs").Scan(&total); err != nil {
		return 0, 0, err
	}
	return perConversation, total, nil
}

func (s *Store) nextSubagentSequenceLocked(runID string) (uint64, error) {
	var sequence uint64
	if err := s.db.QueryRow("SELECT coalesce(max(sequence),0) FROM subagent_events WHERE run_id=?", runID).Scan(&sequence); err != nil {
		return 0, err
	}
	return sequence + 1, nil
}
