package agent

import (
	"encoding/json"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"unicode/utf8"
)

func bounded(s string) string {
	if len(s) <= 16384 {
		return s
	}
	s = s[:16384]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…（记录已截断）"
}

func (m *Manager) recordTool(id, kind string, raw json.RawMessage) error {
	var v struct {
		ToolCallID string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Args       json.RawMessage `json:"args"`
		Result     json.RawMessage `json:"result"`
		IsError    bool            `json:"isError"`
	}
	if json.Unmarshal(raw, &v) != nil || v.ToolCallID == "" {
		return nil
	}
	return m.store.Update(func(st *workbench.State) error {
		_, x, ok := executionFor(*st, id)
		if !ok {
			return nil
		}
		index := -1
		for i := range x.Tools {
			if x.Tools[i].ID == v.ToolCallID {
				index = i
				break
			}
		}
		if index < 0 {
			if len(x.Tools) >= 200 {
				return nil
			}
			x.Tools = append(x.Tools, workbench.ToolRecord{ID: v.ToolCallID, Name: v.ToolName})
			index = len(x.Tools) - 1
		}
		if kind == "tool_execution_start" {
			x.Tools[index].Input = bounded(string(v.Args))
		} else {
			x.Tools[index].Result = bounded(string(v.Result))
			x.Tools[index].Failed = v.IsError
		}
		st.Executions[x.ID] = x
		return nil
	})
}
