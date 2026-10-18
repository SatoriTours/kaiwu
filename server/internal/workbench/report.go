package workbench

import (
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

type ReportCheck struct {
	Title  string `json:"title"`
	Claim  string `json:"claim"`
	ToolID string `json:"toolId,omitempty"`
}
type Report struct {
	Summary  string        `json:"summary"`
	Stage    string        `json:"stage"`
	Plan     []string      `json:"plan"`
	Results  []string      `json:"results"`
	Checks   []ReportCheck `json:"checks"`
	Blockers []string      `json:"blockers"`
	Next     []string      `json:"next"`
	Question string        `json:"question"`
}
type Round struct {
	ID          string       `json:"id"`
	Phase       string       `json:"phase"`
	StartedAt   string       `json:"startedAt"`
	FinishedAt  string       `json:"finishedAt,omitempty"`
	Report      *Report      `json:"report,omitempty"`
	ReportError string       `json:"reportError,omitempty"`
	Tools       []ToolRecord `json:"tools"`
	// Profile is the profile this round actually runs with, recorded at round
	// start: the pending next-round request counts, a later switch does not.
	Profile    Profile `json:"profile"`
	Origin     string  `json:"origin,omitempty"`
	DeliveryID string  `json:"deliveryId,omitempty"`
}

const ReportInstructions = `
开悟展示约定：对用户只给简短结论、必要问题和阻塞说明，过程归入记录，不重复长篇操作过程。在方案改变、阶段推进或本轮结束时，在回复末尾附一个完整的 <kaiwu-report>JSON</kaiwu-report> 块。JSON 仅含 summary（600字以内）、stage（planning/developing/verifying/blocked/awaiting_review）、plan（步骤字符串数组）、results（已实现成果字符串数组）、checks（{title,claim,toolId}数组，claim仅passed/failed/not_run/unknown，toolId引用本轮实际工具调用ID，没有则留空）、blockers（字符串数组）、next（字符串数组）、question（需要用户决定的问题，无则空字符串）。没有依据时写未执行或unknown，不能编造验证；执行结束不等于用户确认或Git合并。普通沟通没有结构化进展可只给简短正常回复，不要为填写报告执行额外命令。`

// Accept only complete validated blocks. Preserve original text on malformed input.
func ParseReport(text string) (*Report, string) {
	const open = "<kaiwu-report>"
	const close = "</kaiwu-report>"
	start := strings.LastIndex(text, open)
	if start < 0 {
		return nil, text
	}
	end := strings.Index(text[start+len(open):], close)
	if end < 0 {
		return nil, text
	}
	end += start + len(open)
	raw := text[start+len(open) : end]
	if len(raw) > 32768 {
		return nil, text
	}
	var r Report
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || !validText(r.Summary, 600) || strings.ContainsRune(r.Summary, 0) {
		return nil, text
	}
	switch r.Stage {
	case "planning", "developing", "verifying", "blocked", "awaiting_review":
	default:
		return nil, text
	}
	valid := func(s string, max int) bool {
		return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
	}
	for _, list := range [][]string{r.Plan, r.Results, r.Blockers, r.Next} {
		if len(list) > 30 {
			return nil, text
		}
		for _, s := range list {
			if !valid(s, 1000) {
				return nil, text
			}
		}
	}
	if !valid(r.Question, 1000) || len(r.Checks) > 30 {
		return nil, text
	}
	for _, c := range r.Checks {
		if !validText(c.Title, 160) || !valid(c.ToolID, 100) {
			return nil, text
		}
		switch c.Claim {
		case "passed", "failed", "not_run", "unknown":
		default:
			return nil, text
		}
	}
	visible := strings.TrimSpace(text[:start] + text[end+len(close):])
	return &r, visible
}
func Evidence(r Round, c ReportCheck) string {
	for _, t := range r.Tools {
		if c.ToolID != "" && t.ID == c.ToolID {
			if t.Failed {
				return "failed"
			}
			if t.Result != "" {
				return "executed"
			}
			return "running"
		}
	}
	return "missing"
}

// roundProfile is the effective profile of the next round: an explicit
// pending request wins over the conversation's stored profile.
func roundProfile(c Conversation) Profile {
	if c.PendingProfile != nil {
		return *c.PendingProfile
	}
	return c.Profile
}
func BeginRound(st *State, conversationID, messageID string) {
	c := st.Conversations[conversationID]
	c.Rounds = append(c.Rounds, Round{ID: messageID, Phase: "running", StartedAt: time.Now().UTC().Format(time.RFC3339), Tools: []ToolRecord{}, Profile: roundProfile(c)})
	st.Conversations[c.ID] = c
}
func UpdateRound(st *State, id, roundID string, fn func(*Round)) {
	c := st.Conversations[id]
	for i := range c.Rounds {
		if c.Rounds[i].ID == roundID {
			fn(&c.Rounds[i])
			st.Conversations[id] = c
			return
		}
	}
}
func ConfirmResult(st *State, c Command) (Receipt, error) {
	t, ok := st.Tasks[c.TaskID]
	if !ok || t.ProjectID != c.ProjectID || t.ConversationID != c.ConversationID {
		return Receipt{}, Missing()
	}
	conv := st.Conversations[t.ConversationID]

	x, exists := st.Executions[t.ExecutionID]
	if !exists || (conv.RunState != "idle" && conv.RunState != "interrupted" && conv.RunState != "failed") || t.Status != "needs_confirmation" || TaskMerging(*st, t.ID) {
		return Receipt{}, Conflict("当前结果尚不能确认")
	}
	// Older finished tasks have no Round. Require persisted completion evidence and
	// an exact version token; interrupted executions can never take this path.
	if len(conv.Rounds) == 0 {
		token := "legacy:" + x.ID + ":" + x.FinishedAt
		if x.Phase != "awaiting_review" || x.FinishedAt == "" || c.RoundID != token {
			return Receipt{}, Conflict("没有已完成且可确认的结果")
		}
		conv.Rounds = append(conv.Rounds, Round{ID: token, Phase: "finished", StartedAt: x.StartedAt, FinishedAt: x.FinishedAt, Tools: append([]ToolRecord{}, x.Tools...)})
	}
	r := &conv.Rounds[len(conv.Rounds)-1]
	if r.ID != c.RoundID || r.Phase != "finished" {
		return Receipt{}, Conflict("轮次已变化，请核对最新结果")
	}
	if x.Phase != "awaiting_review" {
		return Receipt{}, Conflict("执行结果尚未完成")
	}
	r.Phase = "confirmed"
	t.Status = "completed"
	x.Phase = "confirmed"
	st.Executions[x.ID] = x
	st.Tasks[t.ID] = t
	st.Conversations[conv.ID] = conv
	return Receipt{ConversationID: conv.ID}, nil
}
