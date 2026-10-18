package workbench

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func validText(text string, max int) bool {
	return utf8.ValidString(text) && strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= max
}
func newConversation(project, task string, p Profile) Conversation {
	key := id()
	return Conversation{ID: key, ProjectID: project, TaskID: task, SessionID: "kaiwu_" + key, RunState: "idle", Profile: p, Messages: []Message{}}
}
func apply(st *State, c Command) (Receipt, error) {
	r := Receipt{}
	project, exists := st.Projects[c.ProjectID]
	if c.Type != "add_project" && c.Type != "set_default_profile" && !exists {
		return r, Missing()
	}
	if c.Type == "start_task" || c.Type == "send_message" || c.Type == "resume" {
		taskID := c.TaskID
		if taskID == "" {
			taskID = st.Conversations[c.ConversationID].TaskID
		}
		if TaskMerging(*st, taskID) {
			return r, Conflict("任务正在合并，请稍后再发送")
		}
	}
	switch c.Type {
	case "set_default_profile", "set_conversation_profile":
		if !validText(c.Provider, 200) || !validText(c.Model, 300) {
			return r, Invalid("请从 pi 检测结果选择模型")
		}
		switch c.Thinking {
		case "", "off", "minimal", "low", "medium", "high", "xhigh":
		default:
			return r, Invalid("思考级别无效")
		}
		profile := Profile{Provider: c.Provider, Model: c.Model, Thinking: c.Thinking, CredentialSource: "server_pi"}
		if c.Type == "set_default_profile" {
			st.Profile = profile
			st.ProfileConfigured = true
			break
		}
		conv, ok := st.Conversations[c.ConversationID]
		if !ok || conv.ProjectID != c.ProjectID || (c.TaskID != "" && conv.TaskID != c.TaskID) {
			return r, Missing()
		}
		if len(conv.Messages) != 0 || conv.SessionFile != "" || conv.RunState != "idle" || TaskMerging(*st, conv.TaskID) {
			return r, Conflict("会话已启动，保留原模型配置；请为新会话选择模型")
		}
		if task, ok := st.Tasks[conv.TaskID]; ok {
			if task.ExecutionID != "" {
				return r, Conflict("任务已启动，保留原模型配置")
			}
			task.Profile = profile
			st.Tasks[task.ID] = task
		}
		conv.Profile = profile
		st.Conversations[conv.ID] = conv
		r.ConversationID = conv.ID
	case "add_project":
		if !validText(c.Name, 80) || !filepath.IsAbs(c.Path) || len(st.Projects) >= 100 {
			return r, Invalid("项目名称／绝对路径无效，或项目已达上限")
		}
		for _, p := range st.Projects {
			if p.Path == c.Path {
				return r, Conflict("项目已登记")
			}
		}
		key := id()
		conversation := newConversation(key, "", st.Profile)
		st.Conversations[conversation.ID] = conversation
		st.Projects[key] = Project{ID: key, Name: strings.TrimSpace(c.Name), Path: c.Path, ConversationID: conversation.ID}
		st.Topics = append(st.Topics, Topic{key + "_other", key, "其他", true})
		r.CreatedID = key
	case "update_project_git":
		for _, op := range st.Merges {
			if op.ProjectID == project.ID && MergeActive(op.Phase) {
				return r, Conflict("项目正在合并，请结束后再修改 Git 设置")
			}
		}
		if len(c.MainBranchOverride) > 200 || len(c.CommitName) > 100 || len(c.CommitEmail) > 200 || strings.ContainsAny(c.CommitName+c.CommitEmail, "\r\n\x00<>") {
			return r, Invalid("Git 设置无效")
		}
		project.MainBranchOverride = c.MainBranchOverride
		project.CommitName = c.CommitName
		project.CommitEmail = c.CommitEmail
		st.Projects[project.ID] = project
	case "add_topic":
		if !validText(c.Name, 40) {
			return r, Invalid("专题名需为 1–40 字")
		}
		count := 0
		for _, t := range st.Topics {
			if t.ProjectID == c.ProjectID {
				count++
				if t.Name == strings.TrimSpace(c.Name) {
					return r, Conflict("已有同名专题")
				}
			}
		}
		if count >= 100 {
			return r, Invalid("专题已达上限")
		}
		key := id()
		topic := Topic{key, c.ProjectID, strings.TrimSpace(c.Name), false}
		for i, t := range st.Topics {
			if t.ID == c.ProjectID+"_other" {
				st.Topics = append(st.Topics[:i], append([]Topic{topic}, st.Topics[i:]...)...)
				break
			}
		}
		r.CreatedID = key
	case "delete_topic":
		index := -1
		for i, t := range st.Topics {
			if t.ID == c.TopicID && t.ProjectID == c.ProjectID {
				if t.Protected {
					return r, Invalid("其他专题不能删除")
				}
				index = i
				break
			}
		}
		if index < 0 {
			return r, Missing()
		}
		for key, t := range st.Tasks {
			if t.ProjectID == c.ProjectID && t.TopicID == c.TopicID {
				t.TopicID = c.ProjectID + "_other"
				st.Tasks[key] = t
			}
		}
		st.Topics = append(st.Topics[:index], st.Topics[index+1:]...)
	case "add_task":
		if !validText(c.Request, 5000) || utf8.RuneCountInString(c.Title) > 80 {
			return r, Invalid("需求需为 1–5000 字，标题至多 80 字")
		}
		count := 0
		for _, t := range st.Tasks {
			if t.ProjectID == c.ProjectID {
				count++
			}
		}
		if count >= 1000 {
			return r, Invalid("任务已达上限")
		}
		topic := c.ProjectID + "_other"
		for _, t := range st.Topics {
			if t.ID == c.TopicID {
				if t.ProjectID != c.ProjectID {
					return r, Missing()
				}
				topic = t.ID
			}
		}
		if c.TopicID != "" && topic != c.TopicID {
			r.Notice = "原专题已不存在，任务已放入其他"
		}
		key := id()
		conversation := newConversation(project.ID, key, st.Profile)
		st.Conversations[conversation.ID] = conversation
		title := strings.TrimSpace(c.Title)
		if title == "" {
			v := []rune(strings.TrimSpace(c.Request))
			if len(v) > 20 {
				v = v[:20]
			}
			title = string(v)
		}
		st.Tasks[key] = Task{ID: key, ProjectID: project.ID, TopicID: topic, Title: title, Request: strings.TrimSpace(c.Request), Status: "idle", ConversationID: conversation.ID, Profile: st.Profile}
		r.CreatedID = key
	case "confirm_result":
		return ConfirmResult(st, c)
	case "answer_interaction":
		return answerInteraction(st, c)
	case "cancel_interaction":
		return cancelInteraction(st, c)
	case "retry_interaction_answer":
		return retryInteractionAnswer(st, c)
	case "start_task":
		return startTask(st, c)
	case "send_message", "resume":
		conversation, ok := st.Conversations[c.ConversationID]
		if !ok || conversation.ProjectID != c.ProjectID || (c.TaskID != "" && c.TaskID != conversation.TaskID) {
			return r, Missing()
		}
		// A cycle that is waiting for the user's answer must accept that new
		// instruction; an actively working cycle still refuses chat turns.
		if summary := st.Deliveries[conversation.TaskID]; DeliveryActive(summary.Phase) && summary.Phase != "paused" && !strings.HasPrefix(summary.WaitingCode, "interaction_") {
			return r, Conflict("自动交付正在管理该候选，请先暂停交付再发送新消息或接回会话")
		}
		if c.Type == "send_message" {
			if hasRecordedAnswerAwaitingBatch(conversation) {
				return r, Conflict("还有已记录的问题答复待一起发送，请先回答或取消剩余问题")
			}
			if hasUnsentInteractionAnswer(*st, conversation.ID) {
				return r, Conflict("还有已记录的问题答复未送达，请先重试发送")
			}
		}
		if conversation.RunState == "running" || conversation.RunState == "starting" {
			return r, Conflict("会话正在运行，草稿已保留")
		}
		if c.Type == "send_message" {
			if !validText(c.Text, 5000) || len(conversation.Messages) >= 2000 {
				return r, Invalid("消息为空／过长，或会话消息已达上限")
			}
			key := id()
			conversation.Messages = append(conversation.Messages, Message{ID: key, Role: "user", Text: c.Text, Delivery: "pending"})
			r.CreatedID = key
		}
		conversation.RunState = "starting"
		conversation.Error = ""
		st.Conversations[conversation.ID] = conversation
		if c.Type == "send_message" && conversation.TaskID != "" {
			t := st.Tasks[conversation.TaskID]
			if t.ExecutionID != "" {
				t.Status = "running"
				st.Tasks[t.ID] = t
				x := st.Executions[t.ExecutionID]
				x.Phase = "queued"
				x.Error = ""
				x.FinishedAt = ""
				st.Executions[x.ID] = x
			}
		}
		r.ConversationID = conversation.ID
	default:
		return r, Invalid("不支持的操作")
	}
	return r, nil
}
