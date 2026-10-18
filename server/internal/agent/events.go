package agent

import (
	"encoding/json"
	"fmt"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"strings"
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
			m.stopFailed(id, entry, client, "pi 会话已中断，发送下一条消息时自动接回；历史已保留", true)
			return
		}
		m.mu.Lock()
		dead := entry.dead
		m.mu.Unlock()
		if dead {
			return
		}
		// Extension UI requests carry their own "message" string field, so they
		// must be routed before the message-shape probe below.
		if event.Type == extensionUIRequestEvent {
			m.handleExtensionUI(id, entry, client, event.Raw)
			continue
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
			m.mu.Lock()
			roundID := entry.messageID
			m.mu.Unlock()
			e = m.recordTool(id, event.Type, event.Raw, roundID)
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
				entry.responseSequence++
				entry.responseID = entry.messageID + "_reply"
				if entry.responseSequence > 1 {
					entry.responseID += fmt.Sprint("_", entry.responseSequence)
				}
			}
			responseID := entry.responseID
			roundID := entry.messageID
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
				c.Messages[index].DisplayText = nil
				visible := c.Messages[index].Text
				if report, stripped := workbench.ParseReport(visible); report != nil {
					visible = stripped
					workbench.UpdateRound(st, id, roundID, func(r *workbench.Round) { r.Report = report; r.ReportError = "" })
					c.Rounds = st.Conversations[id].Rounds
				}
				// Hide a complete structured block while streaming too; the
				// request is only published at the settled boundary.
				if block, stripped := workbench.ParseInteractionBlock(visible); block != nil {
					visible = stripped
				}
				if visible != c.Messages[index].Text {
					c.Messages[index].DisplayText = &visible
				}
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
				responseID := entry.responseID
				roundID := entry.messageID
				if responseID != "" {
					entry.lastResponseID = responseID
					entry.responseID = ""
				}
				e = m.store.Update(func(st *workbench.State) error {
					c := st.Conversations[id]
					for i := range c.Messages {
						if c.Messages[i].ID == responseID {
							c.Messages[i].Kind = "progress"
							c.Messages[i].Delivery = "complete"
							if strings.Contains(c.Messages[i].Text, "<kaiwu-report>") {
								if report, _ := workbench.ParseReport(c.Messages[i].Text); report == nil {
									c.Messages[i].DisplayText = nil
									for j := range c.Rounds {
										if c.Rounds[j].ID == roundID {
											c.Rounds[j].Report = nil
											c.Rounds[j].ReportError = "本轮最新报告格式无效，完整回复已保留"
										}
									}
								}
							}
							// A malformed interaction block shows the original
							// text until the boundary marks the round fail-closed.
							if strings.Contains(c.Messages[i].Text, interactionTag) {
								if block, stripped := workbench.ParseInteractionBlock(c.Messages[i].Text); block != nil {
									c.Messages[i].DisplayText = &stripped
								} else {
									c.Messages[i].DisplayText = nil
								}
							}
						}
					}
					st.Conversations[id] = c
					return nil
				})
				m.mu.Unlock()
				if e != nil {
					m.stopFailed(id, entry, client, "无法保存消息结束状态，请检查存储后恢复", true)
					return
				}
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
			m.flushSettledInteractionAnswers(id)
		}
	}
}
