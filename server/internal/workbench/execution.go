package workbench

import (
	"strings"
	"time"
)

func IsStartRequest(text string) bool {
	return strings.TrimRight(strings.TrimSpace(text), "。！!.") == "开始开发"
}

// Older restore-only code could mark an unfinished execution awaiting_review.
// A persisted finished round is evidence; a phase label alone is not.
func unfinishedExecution(st State, t Task) bool {
	x, ok := st.Executions[t.ExecutionID]
	if !ok {
		return false
	}
	if x.Phase == "interrupted" {
		return true
	}
	rounds := st.Conversations[t.ConversationID].Rounds
	finished := len(rounds) > 0 && (rounds[len(rounds)-1].Phase == "finished" || rounds[len(rounds)-1].Phase == "confirmed")
	return x.Phase == "awaiting_review" && x.FinishedAt == "" && !finished
}

func startTask(st *State, c Command) (Receipt, error) {
	if c.TaskID == "" {
		return Receipt{}, Invalid("请先选择一张任务卡片，再开始开发")
	}
	t, ok := st.Tasks[c.TaskID]
	if !ok || t.ProjectID != c.ProjectID || (c.ConversationID != "" && c.ConversationID != t.ConversationID) {
		return Receipt{}, Missing()
	}
	conv := st.Conversations[t.ConversationID]
	if conv.RunState == "starting" || conv.RunState == "running" {
		return Receipt{}, Conflict("任务会话正在运行，请等待当前轮次结束")
	}
	continuing := t.ExecutionID != "" && (t.Status == "interrupted" || (t.Status == "needs_confirmation" && unfinishedExecution(*st, t)))
	if t.ExecutionID != "" && t.Status != "failed" && !continuing && !(t.Status == "idle" && st.Executions[t.ExecutionID].Phase == "ready") {
		return Receipt{}, Conflict("该任务已开始；可在原对话继续沟通或查看执行结果")
	}
	if conv.Profile.Model == "" && len(conv.Messages) == 0 {
		conv.Profile = st.Profile
	}
	if conv.Profile.Provider == "" || conv.Profile.Model == "" {
		return Receipt{}, Invalid("服务尚未配置 pi 模型，任务未启动")
	}
	if len(conv.Messages) >= 2000 {
		return Receipt{}, Invalid("会话消息已达上限")
	}
	text := c.Text
	if text == "" {
		text = "开始开发"
	}
	if !IsStartRequest(text) {
		return Receipt{}, Invalid("开始开发指令无效")
	}
	key := id()
	x := Execution{ID: key, TaskID: t.ID, ConversationID: conv.ID, Phase: "queued", StartedAt: time.Now().UTC().Format(time.RFC3339), Profile: conv.Profile, Tools: []ToolRecord{}}
	if t.ExecutionID != "" {
		old := st.Executions[t.ExecutionID]
		x.Workspace = old.Workspace
		x.Branch = old.Branch
		x.BaseSHA = old.BaseSHA
	}

	// An explicit continue action reconnects the same execution; preserve all
	// earlier tools and workspace identity, but never resend the old user message.
	if continuing {
		x = st.Executions[t.ExecutionID]
		key = x.ID
		x.Phase = "queued"
		x.Error = ""
		x.FinishedAt = ""
	}
	if st.Executions == nil {
		st.Executions = map[string]Execution{}
	}
	st.Executions[key] = x
	t.ExecutionID = key
	t.Status = "running"
	t.Profile = conv.Profile
	message := id()
	conv.Messages = append(conv.Messages, Message{ID: message, Role: "user", Text: text, Delivery: "pending"})
	conv.RunState = "starting"
	conv.Error = ""
	st.Tasks[t.ID] = t
	st.Conversations[conv.ID] = conv
	return Receipt{CreatedID: message, ConversationID: conv.ID}, nil
}
