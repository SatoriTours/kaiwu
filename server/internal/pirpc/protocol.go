package pirpc

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

type record struct {
	Response *Response
	Event    *Event
}

func commandName(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func jsonObject(raw []byte) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '{' && utf8.Valid(raw) && json.Valid(raw)
}
func encodeCommand(id string, c Command, max int) ([]byte, error) {
	if id == "" || !commandName(c.Type) || c.Type == "extension_ui_response" {
		return nil, ErrInvalidInput
	}
	p := c.Params
	if p == nil {
		p = json.RawMessage(`{}`)
	}
	if len(p) > max {
		return nil, ErrRecordTooLarge
	}
	if !jsonObject(p) {
		return nil, ErrInvalidInput
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(p, &v) != nil {
		return nil, ErrInvalidInput
	}
	if _, ok := v["id"]; ok {
		return nil, ErrInvalidInput
	}
	if _, ok := v["type"]; ok {
		return nil, ErrInvalidInput
	}
	v["id"], _ = json.Marshal(id)
	v["type"], _ = json.Marshal(c.Type)
	b, err := json.Marshal(v)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if len(b) > max {
		return nil, ErrRecordTooLarge
	}
	return append(b, '\n'), nil
}
func decodeRecord(raw []byte) (record, error) {
	if !jsonObject(raw) {
		return record{}, ErrProtocol
	}
	var v map[string]json.RawMessage
	if json.Unmarshal(raw, &v) != nil {
		return record{}, ErrProtocol
	}
	var typ string
	if json.Unmarshal(v["type"], &typ) != nil || typ == "" {
		return record{}, ErrProtocol
	}
	if typ != "response" {
		return record{Event: &Event{Type: typ, Raw: append(json.RawMessage(nil), raw...)}}, nil
	}
	r := &Response{Raw: append(json.RawMessage(nil), raw...)}
	if json.Unmarshal(v["id"], &r.ID) != nil || r.ID == "" || json.Unmarshal(v["command"], &r.Command) != nil || r.Command == "" {
		return record{}, ErrProtocol
	}
	success, ok := v["success"]
	if !ok || !(bytes.Equal(success, []byte("true")) || bytes.Equal(success, []byte("false"))) {
		return record{}, ErrProtocol
	}
	r.Success = bytes.Equal(success, []byte("true"))
	r.Data = v["data"]
	if !r.Success {
		val, ok := v["error"]
		if !ok || bytes.Equal(val, []byte("null")) || json.Unmarshal(val, &r.ErrorText) != nil {
			return record{}, ErrProtocol
		}
	}
	return record{Response: r}, nil
}
func validateState(data json.RawMessage) error {
	if !jsonObject(data) {
		return ErrProtocol
	}
	var state struct {
		SessionID  string `json:"sessionId"`
		Streaming  *bool  `json:"isStreaming"`
		Compacting *bool  `json:"isCompacting"`
		Pending    *int64 `json:"pendingMessageCount"`
	}
	if json.Unmarshal(data, &state) != nil || state.SessionID == "" || state.Streaming == nil || state.Compacting == nil || state.Pending == nil || *state.Pending < 0 {
		return ErrProtocol
	}
	return nil
}
