package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// This file adapts the pi extension UI sub-protocol (rpc-extension-ui.md,
// pi 1.3.0) onto the product interaction model. Only the four dialog methods
// that really block a wait are mapped; fire-and-forget methods (notify,
// setStatus, setWidget, setTitle, set_editor_text) never produce a form.

const extensionUIRequestEvent = "extension_ui_request"

type extensionUIRequest struct {
	ID          string   `json:"id"`
	Method      string   `json:"method"`
	Title       string   `json:"title"`
	Message     string   `json:"message"`
	Placeholder string   `json:"placeholder"`
	Prefill     string   `json:"prefill"`
	Options     []string `json:"options"`
	Timeout     *int64   `json:"timeout"`
}

func newRunToken() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return "run_" + hex.EncodeToString(b)
}

func validUpstreamID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// mapExtensionUI converts one upstream dialog into product fields. ok=false
// means the request cannot be represented within product limits: the caller
// cancels it upstream rather than pretending it was shown.
func mapExtensionUI(req extensionUIRequest) (workbench.InteractionDefinition, *workbench.InteractionNative, string, bool) {
	native := &workbench.InteractionNative{Method: req.Method, UpstreamID: req.ID, HasTimeout: req.Timeout != nil}
	def := workbench.InteractionDefinition{AllowCancel: true}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		switch req.Method {
		case "confirm":
			title = "AI 请求确认"
		case "select":
			title = "AI 请求选择"
		case "input":
			title = "AI 请求输入"
		default:
			title = "AI 请求编辑"
		}
	}
	if utf8.RuneCountInString(title) > workbench.MaxInteractionTitle {
		return def, native, "原生请求标题超过 160 字，未展示以免隐藏决策内容；已取消该等待", false
	}
	def.Title = title
	switch req.Method {
	case "confirm":
		message := strings.TrimSpace(req.Message)
		if utf8.RuneCountInString(message) > workbench.MaxInteractionDescription {
			return def, native, "原生确认说明超过 4000 字，未展示以免隐藏决策内容；已取消该等待", false
		}
		label := title
		if message != "" {
			// A long decision text is never cut: the short label stays generic
			// and the complete text remains visible in the description.
			label = "请确认"
			if utf8.RuneCountInString(message) <= workbench.MaxInteractionTitle {
				label = message
			}
		}
		def.Description = message
		def.Fields = []workbench.InteractionField{{ID: "confirmed", Type: workbench.FieldConfirm, Label: label, Required: true, YesLabel: "允许", NoLabel: "不允许"}}
	case "select":
		if len(req.Options) == 0 || len(req.Options) > workbench.MaxInteractionOptions {
			return def, native, "原生选项数量超出 1–20，未展示；已取消该等待", false
		}
		options := make([]workbench.InteractionOption, 0, len(req.Options))
		for i, label := range req.Options {
			trimmed := strings.TrimSpace(label)
			if trimmed == "" || !utf8.ValidString(trimmed) || strings.ContainsRune(trimmed, 0) {
				return def, native, "原生选项内容无效，未展示；已取消该等待", false
			}
			if utf8.RuneCountInString(trimmed) > workbench.MaxInteractionTitle {
				return def, native, "原生选项文本超过 160 字，未展示以免隐藏决策内容；已取消该等待", false
			}
			options = append(options, workbench.InteractionOption{ID: "o" + strconv.Itoa(i), Label: trimmed})
			native.Choices = append(native.Choices, label)
		}
		def.Fields = []workbench.InteractionField{{ID: "value", Type: workbench.FieldSingleSelect, Label: title, Required: true, Options: options}}
	case "input":
		placeholder := strings.TrimSpace(req.Placeholder)
		if utf8.RuneCountInString(placeholder) > 200 {
			// A placeholder is only a hint, never a decision constraint.
			placeholder = ""
		}
		def.Fields = []workbench.InteractionField{{
			ID:          "value",
			Type:        workbench.FieldText,
			Label:       title,
			Placeholder: placeholder,
			Required:    true,
			AllowEmpty:  true,
			MaxLength:   intPointer(workbench.MaxInteractionText),
		}}
	case "editor":
		if !utf8.ValidString(req.Prefill) || strings.ContainsRune(req.Prefill, 0) || utf8.RuneCountInString(req.Prefill) > workbench.MaxInteractionTextarea {
			return def, native, "原生编辑内容超过 5000 字或包含无效字符，未展示以免隐藏决策内容；已取消该等待", false
		}
		def.Fields = []workbench.InteractionField{{
			ID:         "value",
			Type:       workbench.FieldTextarea,
			Label:      title,
			Prefill:    req.Prefill,
			Required:   true,
			AllowEmpty: true,
			MaxLength:  intPointer(workbench.MaxInteractionTextarea),
		}}
	default:
		return def, native, "不支持的原生交互方法 " + req.Method, false
	}
	return def, native, "", true
}

func intPointer(v int) *int { return &v }

// pendingNativeRequest reports whether the same upstream dialog is already
// stored for this run, so duplicate events cannot stack a second form.
func pendingNativeRequest(c workbench.Conversation, upstreamID, runID string) bool {
	for _, r := range c.Interactions {
		if r.Native != nil && r.Native.RunID == runID && r.Native.UpstreamID == upstreamID && r.Status == workbench.InteractionPending {
			return true
		}
	}
	return false
}

// handleExtensionUI stores one dialog request with its run binding. A request
// we cannot represent is cancelled upstream (cancelled:true), which resolves
// the extension's wait without faking support.
func (m *Manager) handleExtensionUI(conversationID string, entry *instance, client *pirpc.Client, raw json.RawMessage) {
	var req extensionUIRequest
	if json.Unmarshal(raw, &req) != nil {
		return
	}
	cancel := func() {
		if !validUpstreamID(req.ID) {
			return
		}
		ctx, done := context.WithTimeout(m.ctx, 5*time.Second)
		_, _ = client.RespondUI(ctx, pirpc.UIResponse{ID: req.ID, Cancelled: true})
		done()
	}
	switch req.Method {
	case "confirm", "select", "input", "editor":
	case "notify", "setStatus", "setWidget", "setTitle", "set_editor_text":
		// Known fire-and-forget methods never block pi and need no form.
		return
	default:
		// An unknown method with a valid id might be a future blocking dialog.
		// Never leave it hanging silently: cancel it upstream and say why.
		if validUpstreamID(req.ID) {
			cancel()
			m.noteNativeNotice(conversationID, entry.messageID, "收到当前版本不支持的原生交互方法 "+req.Method+"，已取消该等待；支持 confirm/select/input/editor")
		}
		return
	}
	if !validUpstreamID(req.ID) {
		return
	}
	def, native, reason, ok := mapExtensionUI(req)
	if !ok {
		cancel()
		m.noteNativeNotice(conversationID, entry.messageID, reason)
		return
	}
	m.mu.Lock()
	runID, generation := entry.runID, entry.generation
	m.mu.Unlock()
	if runID == "" {
		cancel()
		return
	}
	native.RunID, native.Generation = runID, generation
	now := time.Now().UTC()
	expires := ""
	if req.Timeout != nil && *req.Timeout > 0 {
		d := time.Duration(*req.Timeout) * time.Millisecond
		if d <= 24*time.Hour {
			expires = now.Add(d).Format(time.RFC3339)
		}
	}
	c := m.store.Snapshot().Conversations[conversationID]
	dedupe := "agent_ui:" + runID + ":" + req.ID
	sum := sha256.Sum256([]byte(conversationID + "\x00" + dedupe))
	request := workbench.InteractionRequest{
		SchemaVersion:  workbench.InteractionSchemaVersion,
		ID:             "ix_" + hex.EncodeToString(sum[:12]),
		ProjectID:      c.ProjectID,
		ConversationID: conversationID,
		TaskID:         c.TaskID,
		RoundID:        entry.messageID,
		Source:         workbench.SourceAgentUI,
		Title:          def.Title,
		Description:    def.Description,
		Fields:         def.Fields,
		Blocking:       true,
		AllowCancel:    true,
		Status:         workbench.InteractionPending,
		CreatedAt:      now.Format(time.RFC3339),
		ExpiresAt:      expires,
		Native:         native,
		DedupeKey:      dedupe,
	}
	if pendingNativeRequest(c, req.ID, runID) {
		return
	}
	if err := m.store.Update(func(st *workbench.State) error {
		_, _, err := workbench.PublishInteraction(st, request)
		return err
	}); err != nil {
		cancel()
	}
}

// nativeUIResponse builds the exact upstream record from the validated answer.
func nativeUIResponse(req workbench.InteractionRequest) (pirpc.UIResponse, error) {
	if req.Native == nil || req.Response == nil {
		return pirpc.UIResponse{}, workbench.Invalid("原生交互缺少答复")
	}
	native := req.Native
	if req.Response.Mode == workbench.AnswerCancel {
		return pirpc.UIResponse{ID: native.UpstreamID, Cancelled: true}, nil
	}
	switch native.Method {
	case "confirm":
		value, ok := req.Response.Answers["confirmed"].(bool)
		if !ok {
			return pirpc.UIResponse{}, workbench.Invalid("确认答复无效")
		}
		return pirpc.UIResponse{ID: native.UpstreamID, Confirmed: &value}, nil
	case "select":
		option, ok := req.Response.Answers["value"].(string)
		if !ok || !strings.HasPrefix(option, "o") {
			return pirpc.UIResponse{}, workbench.Invalid("选项答复无效")
		}
		index, err := strconv.Atoi(strings.TrimPrefix(option, "o"))
		if err != nil || index < 0 || index >= len(native.Choices) {
			return pirpc.UIResponse{}, workbench.Invalid("选项答复与上游请求不匹配")
		}
		value := native.Choices[index]
		return pirpc.UIResponse{ID: native.UpstreamID, Value: &value}, nil
	case "input", "editor":
		value, ok := req.Response.Answers["value"].(string)
		if !ok {
			return pirpc.UIResponse{}, workbench.Invalid("文本答复无效")
		}
		return pirpc.UIResponse{ID: native.UpstreamID, Value: &value}, nil
	}
	return pirpc.UIResponse{}, workbench.Invalid("不支持的原生交互方法")
}

// AnswerInteraction delivers one stored answer to the original pi waiter.
// It never creates a chat message and never starts a new prompt.
func (m *Manager) AnswerInteraction(conversationID, interactionID string) error {
	req, err := m.store.Interaction(conversationID, interactionID)
	if err != nil {
		return err
	}
	if req.Source != workbench.SourceAgentUI || req.Native == nil {
		return workbench.Invalid("该交互不是原生请求")
	}
	if req.Response == nil || req.Response.Delivery != workbench.DeliveryRecorded {
		return nil
	}
	response, err := nativeUIResponse(*req)
	if err != nil {
		m.markNativeDelivery(conversationID, interactionID, workbench.DeliveryNotSent, err.Error())
		return err
	}
	m.mu.Lock()
	entry := m.instances[conversationID]
	client, runID, generation := (*pirpc.Client)(nil), "", 0
	if entry != nil {
		client, runID, generation = entry.client, entry.runID, entry.generation
	}
	dead := entry != nil && entry.dead
	m.mu.Unlock()
	if client == nil || dead || runID == "" || runID != req.Native.RunID || generation != req.Native.Generation {
		note := "原 pi 等待请求已失效，答复未发送；请让 AI 重新发起"
		m.markNativeDelivery(conversationID, interactionID, workbench.DeliveryNotSent, note)
		_ = m.store.Update(func(st *workbench.State) error {
			workbench.ExpireNativeInteractions(st, conversationID, "原 pi 进程已更换，原生请求失效")
			return nil
		})
		return workbench.Conflict(note)
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	delivery, sendErr := client.RespondUI(ctx, response)
	cancel()
	switch delivery {
	case pirpc.Acknowledged:
		m.markNativeDelivery(conversationID, interactionID, workbench.DeliveryAccepted, "已写入原 pi 等待中的请求")
		// The dialog was actually answered: the user wait ends here, not when
		// the worker later observes the round. The AI runtime after this point
		// must consume the activation budget.
		if c, ok := m.store.Snapshot().Conversations[conversationID]; ok && c.TaskID != "" {
			m.store.NotifyUserTurn(c.TaskID, conversationID)
		}
		return nil
	case pirpc.Uncertain:
		note := "答复可能已写入原 pi，但未确认；不会自动重发，请核对会话"
		m.markNativeDelivery(conversationID, interactionID, workbench.DeliveryUncertain, note)
		return workbench.Conflict(note)
	default:
		note := "答复未写入原 pi，可明确重试发送"
		if sendErr != nil {
			note += "：" + sendErr.Error()
		}
		m.markNativeDelivery(conversationID, interactionID, workbench.DeliveryNotSent, note)
		return workbench.Conflict(note)
	}
}

// noteNativeNotice records a visible capability limit on the round without
// turning automation into a false success or a silent hang.
func (m *Manager) noteNativeNotice(conversationID, roundID, text string) {
	if roundID == "" || strings.TrimSpace(text) == "" {
		return
	}
	_ = m.store.Update(func(st *workbench.State) error {
		c := st.Conversations[conversationID]
		for i := range c.Rounds {
			if c.Rounds[i].ID != roundID {
				continue
			}
			notice := strings.TrimSpace(text)
			if c.Rounds[i].InteractionNotice != "" {
				notice = c.Rounds[i].InteractionNotice + "；" + notice
			}
			c.Rounds[i].InteractionNotice = truncateRunes(notice, 1000)
			st.Conversations[c.ID] = c
			return nil
		}
		return nil
	})
}

func (m *Manager) markNativeDelivery(conversationID, interactionID, delivery, note string) {
	_ = m.store.Update(func(st *workbench.State) error {
		workbench.SetInteractionDelivery(st, conversationID, interactionID, delivery, note)
		return nil
	})
}
