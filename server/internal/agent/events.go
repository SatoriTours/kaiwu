package agent

import (
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func (m *Manager) consume(id string, entry *instance, client *pirpc.Client) {
	defer m.wg.Done()
	defer close(entry.consumerDone)
	for {
		event, e := client.NextEvent(m.ctx)
		if e != nil {
			m.mu.Lock()
			dead := entry.dead
			if dead {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			m.stopFailed(id, entry, client, "pi 会话已中断，请明确恢复；历史已保留", true)
			return
		}
		m.mu.Lock()
		dead := entry.dead
		m.mu.Unlock()
		if dead {
			return
		}
		var v struct {
			AssistantMessageEvent struct{ Type, Delta string }      `json:"assistantMessageEvent"`
			Message               struct{ Role, StopReason string } `json:"message"`
		}
		if json.Unmarshal(event.Raw, &v) != nil {
			continue
		}
		switch event.Type {
		case "tool_execution_start", "tool_execution_end":
			e = m.recordTool(id, event.Type, event.Raw)
			if e != nil {
				m.stopFailed(id, entry, client, "无法保存执行记录，请检查存储后恢复", true)
				return
			}
		case "message_update":
			if v.AssistantMessageEvent.Type != "text_delta" {
				continue
			}
			m.mu.Lock()
			if entry.responseID == "" {
				entry.responseID = entry.messageID + "_reply"
			}
			responseID := entry.responseID
			m.mu.Unlock()
			e = m.store.Update(func(st *workbench.State) error {
				c := st.Conversations[id]
				index := -1
				for i := range c.Messages {
					if c.Messages[i].ID == responseID {
						index = i
					}
				}
				if index < 0 {
					if len(c.Messages) >= 2000 {
						return workbench.Invalid("会话消息已达上限")
					}
					c.Messages = append(c.Messages, workbench.Message{ID: responseID, Role: "assistant", Delivery: "accepted"})
					index = len(c.Messages) - 1
				}
				if len(c.Messages[index].Text)+len(v.AssistantMessageEvent.Delta) > 1<<20 {
					return workbench.Invalid("助手回复超出当前上限")
				}
				c.Messages[index].Text += v.AssistantMessageEvent.Delta
				st.Conversations[id] = c
				return nil
			})
			if e != nil {
				m.stopFailed(id, entry, client, "无法保存完整回复，请检查存储／内容上限后恢复", true)
				return
			}
		case "message_end":
			if v.Message.Role == "assistant" {
				m.mu.Lock()
				entry.lastError = v.Message.StopReason == "error" || v.Message.StopReason == "aborted"
				m.mu.Unlock()
			}
		case "agent_settled":
			conversation := m.store.Snapshot().Conversations[id]
			if m.validateSession(conversation, conversation.SessionFile) != nil {
				m.stopFailed(id, entry, client, "原会话未可靠保存，历史保留；请检查会话文件", true)
				return
			}
			m.mu.Lock()
			entry.settled = true
			e = m.finishLocked(id, entry)
			m.mu.Unlock()
			if e != nil {
				m.stopFailed(id, entry, client, "无法保存轮次结束状态，请检查存储后恢复", true)
				return
			}
		}
	}
}
