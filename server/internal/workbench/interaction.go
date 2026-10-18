package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The interaction protocol is server-owned: browsers and models may only pick
// values from a bounded, validated field set. Routing, identity, source and
// delivery status are never accepted from an AI output block.
const (
	InteractionSchemaVersion = 1

	MaxInteractionFields           = 12
	MaxInteractionOptions          = 20
	MaxInteractionRequestBytes     = 32 << 10
	MaxInteractionAnswerBytes      = 32 << 10
	MaxInteractionTitle            = 160
	MaxInteractionDescription      = 4000
	MaxInteractionText             = 2000
	MaxInteractionTextarea         = 5000
	MaxInteractionComment          = 2000
	MaxInteractionsPerConversation = 100
)

const (
	FieldConfirm      = "confirm"
	FieldText         = "text"
	FieldTextarea     = "textarea"
	FieldSingleSelect = "single_select"
	FieldMultiSelect  = "multi_select"
	FieldNumber       = "number"
)

const (
	InteractionPending    = "pending"
	InteractionAnswered   = "answered"
	InteractionCancelled  = "cancelled"
	InteractionExpired    = "expired"
	InteractionSuperseded = "superseded"
)

const (
	SourceAssistantQuestion = "assistant_question"
	SourceAgentUI           = "agent_ui"
	SourceSystemAction      = "system_action"
)

// Delivery states describe the transport step only. "accepted" never means
// the task advanced or the result is complete.
const (
	DeliveryRecorded    = "recorded"
	DeliveryDispatching = "dispatching"
	DeliveryAccepted    = "accepted"
	DeliveryNotSent     = "not_sent"
	DeliveryUncertain   = "uncertain"
)

const (
	AnswerForm   = "form"
	AnswerText   = "text"
	AnswerCancel = "cancel"
)

type InteractionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

type InteractionField struct {
	ID            string              `json:"id"`
	Type          string              `json:"type"`
	Label         string              `json:"label"`
	Description   string              `json:"description,omitempty"`
	Placeholder   string              `json:"placeholder,omitempty"`
	Unit          string              `json:"unit,omitempty"`
	YesLabel      string              `json:"yesLabel,omitempty"`
	NoLabel       string              `json:"noLabel,omitempty"`
	Prefill       string              `json:"prefill,omitempty"`
	Options       []InteractionOption `json:"options,omitempty"`
	Required      bool                `json:"required,omitempty"`
	AllowEmpty    bool                `json:"allowEmpty,omitempty"`
	AllowOther    bool                `json:"allowOther,omitempty"`
	Integer       bool                `json:"integer,omitempty"`
	MinLength     *int                `json:"minLength,omitempty"`
	MaxLength     *int                `json:"maxLength,omitempty"`
	MinSelections *int                `json:"minSelections,omitempty"`
	MaxSelections *int                `json:"maxSelections,omitempty"`
	Min           *float64            `json:"min,omitempty"`
	Max           *float64            `json:"max,omitempty"`
}

// InteractionNative binds a product request to one live pi extension UI wait.
// RunID is a per-process token: a reused upstream id from a later process can
// never receive an answer meant for the old waiter.
type InteractionNative struct {
	Method     string   `json:"method"`
	UpstreamID string   `json:"upstreamId"`
	RunID      string   `json:"runId"`
	Choices    []string `json:"choices,omitempty"`
	HasTimeout bool     `json:"hasTimeout,omitempty"`
	Generation int      `json:"generation"`
}

type InteractionResponse struct {
	Answers     map[string]any    `json:"answers,omitempty"`
	Others      map[string]string `json:"others,omitempty"`
	OperationID string            `json:"operationId,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Text        string            `json:"text,omitempty"`
	Mode        string            `json:"mode"`
	Delivery    string            `json:"delivery"`
	Note        string            `json:"note,omitempty"`
	MessageID   string            `json:"messageId,omitempty"`
	// FollowUpRoundID is the round created from this answer (ordinary source
	// only). Dependent automated work settles when that round finishes, not
	// when the answer row is written.
	FollowUpRoundID string `json:"followUpRoundId,omitempty"`
	AnsweredAt      string `json:"answeredAt"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

type InteractionRequest struct {
	Native            *InteractionNative   `json:"native,omitempty"`
	Response          *InteractionResponse `json:"response,omitempty"`
	SchemaVersion     int                  `json:"schemaVersion"`
	Version           int                  `json:"version"`
	ProjectID         string               `json:"projectId"`
	ConversationID    string               `json:"conversationId"`
	TaskID            string               `json:"taskId,omitempty"`
	RoundID           string               `json:"roundId,omitempty"`
	SourceMessageID   string               `json:"sourceMessageId,omitempty"`
	Source            string               `json:"source"`
	Title             string               `json:"title"`
	Description       string               `json:"description,omitempty"`
	Fields            []InteractionField   `json:"fields"`
	Status            string               `json:"status"`
	StatusReason      string               `json:"statusReason,omitempty"`
	SupersededBy      string               `json:"supersededBy,omitempty"`
	CreatedAt         string               `json:"createdAt"`
	ExpiresAt         string               `json:"expiresAt,omitempty"`
	DedupeKey         string               `json:"dedupeKey,omitempty"`
	ID                string               `json:"id"`
	Blocking          bool                 `json:"blocking"`
	AllowCancel       bool                 `json:"allowCancel"`
	AllowTextFallback bool                 `json:"allowTextFallback,omitempty"`
}

type InteractionDefinition struct {
	Title       string
	Description string
	Fields      []InteractionField
	AllowCancel bool
}

type ParsedInteraction struct {
	Definition  InteractionDefinition
	Fingerprint string
}

// Submission is one validated user answer. Answers keep protocol values:
// boolean, string, []string or float64. Free text for "other" is separate so
// option ids stay stable.
type Submission struct {
	Answers map[string]any
	Others  map[string]string
	Mode    string
	Comment string
	Text    string
}

// InteractionReceipt flattens receipt fields so existing consumers keep
// working, and adds the authoritative request after the mutation.
type InteractionReceipt struct {
	Receipt
	Interaction *InteractionRequest `json:"interaction,omitempty"`
}

var (
	interactionFieldID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	interactionID      = regexp.MustCompile(`^ix_[A-Za-z0-9_-]{3,64}$`)
	interactionRunID   = regexp.MustCompile(`^[A-Za-z0-9_-]{4,80}$`)
)

func intPtrValue(v int) *int { return &v }

func validOptionalText(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}

func validRequiredText(s string, max int) bool {
	return validText(s, max)
}

// ParseInteractionBlock accepts only one complete, bounded and fully valid
// block. Malformed input is never partially interpreted: the original text is
// returned untouched and the caller must fail closed for automation.
func ParseInteractionBlock(text string) (*ParsedInteraction, string) {
	const open = "<kaiwu-interaction>"
	const close = "</kaiwu-interaction>"
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
	if len(raw) > MaxInteractionRequestBytes {
		return nil, text
	}
	var wire struct {
		Title       string             `json:"title"`
		Description string             `json:"description"`
		Fields      []InteractionField `json:"fields"`
		AllowCancel bool               `json:"allowCancel"`
		Blocking    *bool              `json:"blocking"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&wire) != nil || d.Decode(new(any)) != io.EOF {
		return nil, text
	}
	def := InteractionDefinition{Title: strings.TrimSpace(wire.Title), Description: strings.TrimSpace(wire.Description), Fields: wire.Fields, AllowCancel: wire.AllowCancel}
	if err := normalizeDefinition(&def); err != nil {
		return nil, text
	}
	canonical, _ := json.Marshal(struct {
		Title       string             `json:"title"`
		Description string             `json:"description,omitempty"`
		Fields      []InteractionField `json:"fields"`
		AllowCancel bool               `json:"allowCancel"`
	}{def.Title, def.Description, def.Fields, def.AllowCancel})
	sum := sha256.Sum256(canonical)
	visible := strings.TrimSpace(text[:start] + text[end+len(close):])
	return &ParsedInteraction{Definition: def, Fingerprint: "ixf_" + hex.EncodeToString(sum[:16])}, visible
}

func normalizeDefinition(def *InteractionDefinition) error {
	if !validRequiredText(def.Title, MaxInteractionTitle) || !validOptionalText(def.Description, MaxInteractionDescription) {
		return fmt.Errorf("交互标题或说明无效")
	}
	if len(def.Fields) == 0 || len(def.Fields) > MaxInteractionFields {
		return fmt.Errorf("交互字段必须为 1–%d 个", MaxInteractionFields)
	}
	seen := map[string]bool{}
	for i := range def.Fields {
		f := &def.Fields[i]
		f.ID = strings.TrimSpace(f.ID)
		f.Label = strings.TrimSpace(f.Label)
		f.Description = strings.TrimSpace(f.Description)
		f.Placeholder = strings.TrimSpace(f.Placeholder)
		f.Unit = strings.TrimSpace(f.Unit)
		f.YesLabel = strings.TrimSpace(f.YesLabel)
		f.NoLabel = strings.TrimSpace(f.NoLabel)
		if !interactionFieldID.MatchString(f.ID) || seen[f.ID] {
			return fmt.Errorf("字段标识无效或重复")
		}
		seen[f.ID] = true
		if !validRequiredText(f.Label, MaxInteractionTitle) || !validOptionalText(f.Description, 1000) || !validOptionalText(f.Placeholder, 200) || !validOptionalText(f.Unit, 16) || !validOptionalText(f.YesLabel, 40) || !validOptionalText(f.NoLabel, 40) || !validOptionalText(f.Prefill, MaxInteractionTextarea) {
			return fmt.Errorf("字段展示信息无效")
		}
		if err := normalizeField(f); err != nil {
			return err
		}
	}
	return nil
}

// normalizeField rejects settings that do not apply to the field type instead
// of silently dropping them: a model must not believe it configured a limit
// the product ignores.
func normalizeField(f *InteractionField) error {
	hasOptions := len(f.Options) > 0
	switch f.Type {
	case FieldConfirm:
		if hasOptions || f.AllowOther || f.MinLength != nil || f.MaxLength != nil || f.MinSelections != nil || f.MaxSelections != nil || f.Min != nil || f.Max != nil || f.Integer || f.Placeholder != "" || f.Prefill != "" || f.Unit != "" {
			return fmt.Errorf("确认字段包含不适用的约束")
		}
		if f.YesLabel == "" {
			f.YesLabel = "允许"
		}
		if f.NoLabel == "" {
			f.NoLabel = "不允许"
		}
		f.Required = true
		f.AllowEmpty = false
	case FieldText, FieldTextarea:
		if hasOptions || f.AllowOther || f.MinSelections != nil || f.MaxSelections != nil || f.Min != nil || f.Max != nil || f.Integer || f.Unit != "" || f.YesLabel != "" || f.NoLabel != "" {
			return fmt.Errorf("文本字段包含不适用的约束")
		}
		limit := MaxInteractionText
		if f.Type == FieldTextarea {
			limit = MaxInteractionTextarea
		}
		if f.MaxLength == nil {
			f.MaxLength = intPtrValue(limit)
		}
		if *f.MaxLength <= 0 || *f.MaxLength > limit {
			return fmt.Errorf("文本长度上限无效")
		}
		if f.MinLength == nil {
			f.MinLength = intPtrValue(0)
		}
		if *f.MinLength < 0 || *f.MinLength > *f.MaxLength {
			return fmt.Errorf("文本长度下限无效")
		}
	case FieldSingleSelect, FieldMultiSelect:
		if f.MinLength != nil || f.MaxLength != nil || f.Min != nil || f.Max != nil || f.Integer || f.Unit != "" || f.YesLabel != "" || f.NoLabel != "" || f.Prefill != "" || f.Placeholder != "" {
			return fmt.Errorf("选择字段包含不适用的约束")
		}
		if len(f.Options) == 0 || len(f.Options) > MaxInteractionOptions {
			return fmt.Errorf("选项必须为 1–%d 个", MaxInteractionOptions)
		}
		optionIDs := map[string]bool{}
		for i := range f.Options {
			o := &f.Options[i]
			o.ID = strings.TrimSpace(o.ID)
			o.Label = strings.TrimSpace(o.Label)
			o.Description = strings.TrimSpace(o.Description)
			if !interactionFieldID.MatchString(o.ID) || optionIDs[o.ID] || !validRequiredText(o.Label, MaxInteractionTitle) || !validOptionalText(o.Description, 500) {
				return fmt.Errorf("选项标识或展示无效")
			}
			optionIDs[o.ID] = true
		}
		ceiling := len(f.Options)
		if f.AllowOther {
			ceiling++
		}
		if f.Type == FieldSingleSelect {
			if f.MinSelections != nil && *f.MinSelections != 1 {
				return fmt.Errorf("单选最少选择数只能为 1")
			}
			if f.MaxSelections != nil && *f.MaxSelections != 1 {
				return fmt.Errorf("单选最多选择数只能为 1")
			}
			f.MinSelections = intPtrValue(1)
			f.MaxSelections = intPtrValue(1)
			f.Required = true
		} else {
			if f.MinSelections == nil {
				if f.Required {
					f.MinSelections = intPtrValue(1)
				} else {
					f.MinSelections = intPtrValue(0)
				}
			}
			if f.MaxSelections == nil {
				f.MaxSelections = intPtrValue(ceiling)
			}
			if *f.MinSelections < 0 || *f.MaxSelections < 1 || *f.MinSelections > *f.MaxSelections || *f.MaxSelections > ceiling || *f.MinSelections > ceiling {
				return fmt.Errorf("多选数量限制无效")
			}
		}
	case FieldNumber:
		if hasOptions || f.AllowOther || f.MinLength != nil || f.MaxLength != nil || f.MinSelections != nil || f.MaxSelections != nil || f.YesLabel != "" || f.NoLabel != "" || f.Prefill != "" || f.Placeholder != "" {
			return fmt.Errorf("数字字段包含不适用的约束")
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fmt.Errorf("数字范围无效")
		}
		for _, v := range []*float64{f.Min, f.Max} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return fmt.Errorf("数字范围无效")
			}
		}
	default:
		return fmt.Errorf("不支持的字段类型")
	}
	return nil
}

// ValidateInteractionRequest re-checks a server-built request. Any violation is
// a programming error, never a reason to show a half-valid form.
func ValidateInteractionRequest(r InteractionRequest) error {
	if r.SchemaVersion != InteractionSchemaVersion || !interactionID.MatchString(r.ID) || r.Version < 1 {
		return fmt.Errorf("交互身份无效")
	}
	switch r.Source {
	case SourceAssistantQuestion, SourceAgentUI:
	case SourceSystemAction:
		return fmt.Errorf("系统交互来源尚未启用")
	default:
		return fmt.Errorf("交互来源无效")
	}
	if r.ProjectID == "" || r.ConversationID == "" || r.Title == "" {
		return fmt.Errorf("交互归属无效")
	}
	def := InteractionDefinition{Title: r.Title, Description: r.Description, Fields: r.Fields, AllowCancel: r.AllowCancel}
	if err := normalizeDefinition(&def); err != nil {
		return err
	}
	switch r.Status {
	case InteractionPending, InteractionAnswered, InteractionCancelled, InteractionExpired, InteractionSuperseded:
	default:
		return fmt.Errorf("交互状态无效")
	}
	if r.Status == InteractionAnswered || r.Status == InteractionCancelled {
		if r.Response == nil || !validDelivery(r.Response.Delivery) {
			return fmt.Errorf("交互答复缺失")
		}
	}
	if r.Source == SourceAgentUI {
		if r.Native == nil || r.Native.UpstreamID == "" || !interactionRunID.MatchString(r.Native.RunID) || r.Native.Generation < 1 {
			return fmt.Errorf("原生交互绑定无效")
		}
		switch r.Native.Method {
		case "confirm", "select", "input", "editor":
		default:
			return fmt.Errorf("不支持的原生交互方法")
		}
		if r.AllowTextFallback {
			return fmt.Errorf("原生交互不支持文字替代")
		}
	} else if r.Native != nil {
		return fmt.Errorf("非原生交互不能绑定原生等待者")
	}
	if r.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, r.ExpiresAt); err != nil {
			return fmt.Errorf("交互截止时间无效")
		}
	}
	if _, err := time.Parse(time.RFC3339, r.CreatedAt); err != nil {
		return fmt.Errorf("交互创建时间无效")
	}
	if r.SourceMessageID != "" && utf8.RuneCountInString(r.SourceMessageID) > 100 {
		return fmt.Errorf("来源消息无效")
	}
	return nil
}

func validDelivery(d string) bool {
	switch d {
	case DeliveryRecorded, DeliveryDispatching, DeliveryAccepted, DeliveryNotSent, DeliveryUncertain:
		return true
	}
	return false
}

func answerStrings(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func answerNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func fieldByID(fields []InteractionField, id string) *InteractionField {
	for i := range fields {
		if fields[i].ID == id {
			return &fields[i]
		}
	}
	return nil
}

func optionByID(f *InteractionField, id string) *InteractionOption {
	for i := range f.Options {
		if f.Options[i].ID == id {
			return &f.Options[i]
		}
	}
	return nil
}

// ValidateInteractionSubmission is the single answer validator for HTTP,
// socket and tests: unknown fields, wrong JSON types, missing required values
// and oversized payloads are rejected, not coerced.
func ValidateInteractionSubmission(req InteractionRequest, in Submission) (Submission, error) {
	if in.Mode == "" {
		in.Mode = AnswerForm
	}
	switch in.Mode {
	case AnswerForm:
	case AnswerText:
		if !req.AllowTextFallback || req.Source == SourceAgentUI {
			return Submission{}, Invalid("该请求不支持用文字代替表单回答")
		}
		if len(in.Answers) != 0 || len(in.Others) != 0 {
			return Submission{}, Invalid("文字回答不能再包含表单字段")
		}
		if !validRequiredText(in.Text, MaxInteractionTextarea) {
			return Submission{}, Invalid("请填写文字回答")
		}
		in.Text = strings.TrimSpace(in.Text)
		if !validOptionalText(in.Comment, MaxInteractionComment) {
			return Submission{}, Invalid("补充说明过长")
		}
	case AnswerCancel:
		if !req.AllowCancel {
			return Submission{}, Invalid("该请求不支持取消")
		}
		if len(in.Answers) != 0 || len(in.Others) != 0 || strings.TrimSpace(in.Text) != "" {
			return Submission{}, Invalid("取消请求不能同时提交答案")
		}
		if !validOptionalText(in.Comment, MaxInteractionComment) {
			return Submission{}, Invalid("补充说明过长")
		}
	default:
		return Submission{}, Invalid("答复方式无效")
	}
	if in.Mode != AnswerForm {
		return in, nil
	}
	if !validOptionalText(in.Comment, MaxInteractionComment) {
		return Submission{}, Invalid("补充说明过长")
	}
	for key := range in.Answers {
		if fieldByID(req.Fields, key) == nil {
			return Submission{}, Invalid("答复包含未知字段：" + key)
		}
	}
	for key := range in.Others {
		f := fieldByID(req.Fields, key)
		if f == nil {
			return Submission{}, Invalid("补充输入包含未知字段：" + key)
		}
		if !f.AllowOther || (f.Type != FieldSingleSelect && f.Type != FieldMultiSelect) {
			return Submission{}, Invalid("该字段不允许填写“其他”")
		}
		if strings.TrimSpace(in.Others[key]) == "" || !validOptionalText(in.Others[key], MaxInteractionText) {
			return Submission{}, Invalid("“其他”内容为空或过长")
		}
		in.Others[key] = strings.TrimSpace(in.Others[key])
	}
	normalized := map[string]any{}
	for _, f := range req.Fields {
		value, present := in.Answers[f.ID]
		other := in.Others[f.ID]
		switch f.Type {
		case FieldConfirm:
			if !present {
				return Submission{}, Invalid("请选择“" + f.Label + "”")
			}
			flag, ok := value.(bool)
			if !ok {
				return Submission{}, Invalid("确认字段必须为是或否")
			}
			normalized[f.ID] = flag
		case FieldText, FieldTextarea:
			if other != "" {
				return Submission{}, Invalid("文本字段不能使用“其他”")
			}
			if !present {
				if f.Required && !f.AllowEmpty {
					return Submission{}, Invalid("请填写“" + f.Label + "”")
				}
				continue
			}
			text, ok := value.(string)
			if !ok {
				return Submission{}, Invalid("“" + f.Label + "”必须是文本")
			}
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return Submission{}, Invalid("“" + f.Label + "”包含无效字符")
			}
			length := utf8.RuneCountInString(text)
			min := 0
			if f.MinLength != nil {
				min = *f.MinLength
			}
			max := MaxInteractionText
			if f.MaxLength != nil {
				max = *f.MaxLength
			}
			if length < min || length > max {
				return Submission{}, Invalid("“" + f.Label + "”长度超出限制")
			}
			if f.Required && !f.AllowEmpty && strings.TrimSpace(text) == "" {
				return Submission{}, Invalid("请填写“" + f.Label + "”")
			}
			normalized[f.ID] = text
		case FieldSingleSelect:
			option, hasOption := value.(string)
			if present && !hasOption {
				return Submission{}, Invalid("“" + f.Label + "”选项无效")
			}
			if hasOption && other != "" {
				return Submission{}, Invalid("“" + f.Label + "”不能同时选择选项和填写其他")
			}
			if hasOption {
				if optionByID(&f, option) == nil {
					return Submission{}, Invalid("“" + f.Label + "”选项已失效")
				}
				normalized[f.ID] = option
				continue
			}
			if other == "" {
				return Submission{}, Invalid("请选择“" + f.Label + "”")
			}
		case FieldMultiSelect:
			if !present {
				if other == "" {
					if f.MinSelections != nil && *f.MinSelections > 0 {
						return Submission{}, Invalid("请选择“" + f.Label + "”")
					}
					continue
				}
				normalized[f.ID] = []string{}
				continue
			}
			options, ok := answerStrings(value)
			if !ok {
				return Submission{}, Invalid("“" + f.Label + "”选项无效")
			}
			seen := map[string]bool{}
			count := len(options)
			for _, id := range options {
				if optionByID(&f, id) == nil || seen[id] {
					return Submission{}, Invalid("“" + f.Label + "”选项无效或重复")
				}
				seen[id] = true
			}
			if other != "" {
				count++
			}
			min, max := 0, len(f.Options)
			if f.MinSelections != nil {
				min = *f.MinSelections
			}
			if f.MaxSelections != nil {
				max = *f.MaxSelections
			}
			if count < min {
				return Submission{}, Invalid("“" + f.Label + "”选择数量不足")
			}
			if count > max {
				return Submission{}, Invalid("“" + f.Label + "”选择数量过多")
			}
			normalized[f.ID] = append([]string{}, options...)
		case FieldNumber:
			if !present {
				if f.Required {
					return Submission{}, Invalid("请填写“" + f.Label + "”")
				}
				continue
			}
			number, ok := answerNumber(value)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				return Submission{}, Invalid("“" + f.Label + "”必须是数字")
			}
			if f.Integer && number != math.Trunc(number) {
				return Submission{}, Invalid("“" + f.Label + "”必须是整数")
			}
			if f.Min != nil && number < *f.Min {
				return Submission{}, Invalid("“" + f.Label + "”小于最小值")
			}
			if f.Max != nil && number > *f.Max {
				return Submission{}, Invalid("“" + f.Label + "”大于最大值")
			}
			normalized[f.ID] = number
		}
	}
	in.Answers = normalized
	payload, err := json.Marshal(struct {
		Answers map[string]any    `json:"answers"`
		Others  map[string]string `json:"others,omitempty"`
		Comment string            `json:"comment,omitempty"`
	}{normalized, in.Others, in.Comment})
	if err != nil || len(payload) > MaxInteractionAnswerBytes {
		return Submission{}, Invalid("答复内容超出当前上限")
	}
	return in, nil
}

// ComposeInteractionAnswer renders the durable user turn that answers an
// ordinary assistant question. The structured link stays in the request.
func ComposeInteractionAnswer(req InteractionRequest, sub Submission) string {
	title := strings.TrimSpace(req.Title)
	if sub.Mode == AnswerText {
		return "关于「" + title + "」的答复：\n" + strings.TrimSpace(sub.Text)
	}
	if sub.Mode == AnswerCancel {
		return "关于「" + title + "」：这次不回答，取消本次询问。"
	}
	var b strings.Builder
	b.WriteString("关于「" + title + "」的答复：\n")
	for _, f := range req.Fields {
		value, present := sub.Answers[f.ID]
		other := sub.Others[f.ID]
		switch f.Type {
		case FieldConfirm:
			flag, _ := value.(bool)
			if flag {
				b.WriteString("- " + f.Label + "：" + f.YesLabel + "\n")
			} else {
				b.WriteString("- " + f.Label + "：" + f.NoLabel + "\n")
			}
		case FieldSingleSelect:
			if other != "" {
				b.WriteString("- " + f.Label + "：其他 — " + other + "\n")
				continue
			}
			id, _ := value.(string)
			if option := optionByID(&f, id); option != nil {
				b.WriteString("- " + f.Label + "：" + option.Label + "\n")
			}
		case FieldMultiSelect:
			options, _ := answerStrings(value)
			labels := []string{}
			for _, id := range options {
				if option := optionByID(&f, id); option != nil {
					labels = append(labels, option.Label)
				}
			}
			if other != "" {
				labels = append(labels, "其他 — "+other)
			}
			if len(labels) == 0 {
				b.WriteString("- " + f.Label + "：未选择\n")
				continue
			}
			b.WriteString("- " + f.Label + "：" + strings.Join(labels, "、") + "\n")
		case FieldNumber:
			if number, ok := answerNumber(value); ok {
				formatted := strconv.FormatFloat(number, 'f', -1, 64)
				if f.Unit != "" {
					formatted += " " + f.Unit
				}
				b.WriteString("- " + f.Label + "：" + formatted + "\n")
			}
		default:
			if text, ok := value.(string); ok && (text != "" || present) {
				b.WriteString("- " + f.Label + "：" + text + "\n")
			}
		}
	}
	if strings.TrimSpace(sub.Comment) != "" {
		b.WriteString("补充说明：" + strings.TrimSpace(sub.Comment) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// State transitions. All of these are pure functions on a cloned State so
// callers can persist the answer and its dispatch intent in one transaction.

func interactionIndex(c Conversation, id string) int {
	for i := range c.Interactions {
		if c.Interactions[i].ID == id {
			return i
		}
	}
	return -1
}

// errInteractionExpired marks a lazy timeout: the answer is refused, but the
// expiry transition itself is persisted by the caller.
var errInteractionExpired = errors.New("interaction expired")

func interactionExpiredAt(r InteractionRequest, now time.Time) bool {
	if r.ExpiresAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, r.ExpiresAt)
	return err == nil && !now.Before(at)
}

// PublishInteraction inserts a boundary-scoped request. Re-publishing the same
// source with identical content is a no-op; changed content supersedes the
// older pending version instead of silently replacing it.
func PublishInteraction(st *State, req InteractionRequest) (InteractionRequest, bool, error) {
	if req.Version == 0 {
		req.Version = 1
	}
	if req.Status == "" {
		req.Status = InteractionPending
	}
	if req.CreatedAt == "" {
		req.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := ValidateInteractionRequest(req); err != nil {
		return InteractionRequest{}, false, Invalid(err.Error())
	}
	// Persist the normalized shape so the browser never has to guess defaults.
	def := InteractionDefinition{Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description), Fields: req.Fields, AllowCancel: req.AllowCancel}
	if err := normalizeDefinition(&def); err != nil {
		return InteractionRequest{}, false, Invalid(err.Error())
	}
	req.Title, req.Description, req.Fields = def.Title, def.Description, def.Fields
	c, ok := st.Conversations[req.ConversationID]
	if !ok || c.ProjectID != req.ProjectID || (req.TaskID != "" && c.TaskID != req.TaskID) {
		return InteractionRequest{}, false, Missing()
	}
	version := 1
	for i := range c.Interactions {
		existing := &c.Interactions[i]
		if existing.DedupeKey == "" || existing.DedupeKey != req.DedupeKey || existing.Source != req.Source {
			continue
		}
		if interactionFingerprint(*existing) == interactionFingerprint(req) && existing.Status != InteractionSuperseded {
			return *existing, false, nil
		}
		if existing.Status == InteractionPending {
			existing.Status = InteractionSuperseded
			existing.SupersededBy = req.ID
			existing.StatusReason = "已被同一来源的新版本取代"
		}
		version = existing.Version + 1
	}
	req.Version = version
	if len(c.Interactions) >= MaxInteractionsPerConversation {
		drop := -1
		for i := range c.Interactions {
			if c.Interactions[i].Status != InteractionPending {
				drop = i
				break
			}
		}
		if drop < 0 {
			return InteractionRequest{}, false, Conflict("待处理请求已达上限")
		}
		c.Interactions = append(c.Interactions[:drop], c.Interactions[drop+1:]...)
	}
	c.Interactions = append(c.Interactions, req)
	st.Conversations[c.ID] = c
	return req, true, nil
}

func interactionFingerprint(r InteractionRequest) string {
	def := InteractionDefinition{Title: r.Title, Description: r.Description, Fields: r.Fields, AllowCancel: r.AllowCancel}
	b, _ := json.Marshal(def)
	sum := sha256.Sum256(b)
	return "ixf_" + hex.EncodeToString(sum[:16])
}

// HasPendingNativeInteraction reports whether pi is currently blocked on an
// extension dialog. Such a wait is user time even while the conversation's
// runState is running: the round is active but the model is not.
func HasPendingNativeInteraction(st State, conversationID string) bool {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return false
	}
	now := time.Now().UTC()
	for _, r := range c.Interactions {
		if r.Source == SourceAgentUI && r.Status == InteractionPending && r.Blocking && !interactionExpiredAt(r, now) {
			return true
		}
	}
	return false
}

// PendingBlockingInteraction returns the oldest answerable blocking request of
// one conversation. Requests past their deadline no longer block; they are
// materialized as expired when the user tries to answer or on restart.
func PendingBlockingInteraction(st State, conversationID string) (InteractionRequest, bool) {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return InteractionRequest{}, false
	}
	now := time.Now().UTC()
	for _, r := range c.Interactions {
		if r.Status != InteractionPending || !r.Blocking || interactionExpiredAt(r, now) {
			continue
		}
		return r, true
	}
	return InteractionRequest{}, false
}

// InteractionGate is the server-side block for dependent work: pending
// blocking questions stop result confirmation, development, merges and
// automated delivery; a malformed interaction block on the cycle's round is
// fail-closed too.
func InteractionGate(st State, taskID, roundID string) (code, reason string) {
	t, ok := st.Tasks[taskID]
	if !ok {
		return "", ""
	}
	if _, pending := PendingBlockingInteraction(st, t.ConversationID); pending {
		return "interaction_pending", "还有待你处理的交互问题，回答或取消后才能继续"
	}
	if roundID != "" {
		for _, r := range st.Conversations[t.ConversationID].Rounds {
			if r.ID != roundID {
				continue
			}
			if r.InteractionError != "" {
				return "interaction_invalid", "本轮交互结构无效，自动流程已暂停：" + r.InteractionError
			}
			break
		}
	}
	return "", ""
}

func ExpireNativeInteractions(st *State, conversationID, reason string) int {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return 0
	}
	changed := 0
	for i := range c.Interactions {
		r := &c.Interactions[i]
		if r.Source != SourceAgentUI || r.Status != InteractionPending {
			continue
		}
		r.Status = InteractionExpired
		r.StatusReason = reason
		changed++
	}
	if changed > 0 {
		st.Conversations[c.ID] = c
	}
	return changed
}

func interactionRequest(st *State, conversationID, requestID string) (*InteractionRequest, *Conversation, int, error) {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return nil, nil, 0, Missing()
	}
	index := interactionIndex(c, requestID)
	if index < 0 {
		return nil, nil, 0, Missing()
	}
	return &c.Interactions[index], &c, index, nil
}

func saveInteraction(st *State, c *Conversation, index int, req InteractionRequest) {
	c.Interactions[index] = req
	st.Conversations[c.ID] = *c
}

func locateInteraction(st *State, cmd Command) (InteractionRequest, *Conversation, int, error) {
	c, ok := st.Conversations[cmd.ConversationID]
	if !ok || c.ProjectID != cmd.ProjectID || (cmd.TaskID != "" && c.TaskID != cmd.TaskID) {
		return InteractionRequest{}, nil, 0, Missing()
	}
	index := interactionIndex(c, cmd.RequestID)
	if index < 0 {
		return InteractionRequest{}, nil, 0, Missing()
	}
	req := c.Interactions[index]
	if req.TaskID != "" && cmd.TaskID != "" && req.TaskID != cmd.TaskID {
		return InteractionRequest{}, nil, 0, Missing()
	}
	return req, &c, index, nil
}

func interactionVersion(req InteractionRequest, cmd Command) error {
	if req.Version != cmd.RequestVersion {
		return Conflict("交互请求已更新，请刷新后重试")
	}
	return nil
}

func markExpired(st *State, c *Conversation, index int, req InteractionRequest) {
	req.Status = InteractionExpired
	req.StatusReason = "请求已超时，未自动选择任何答案"
	req.Response = nil
	saveInteraction(st, c, index, req)
}

func answerInteraction(st *State, cmd Command) (Receipt, error) {
	req, c, index, err := locateInteraction(st, cmd)
	if err != nil {
		return Receipt{}, err
	}
	if err = interactionVersion(req, cmd); err != nil {
		return Receipt{}, err
	}
	if req.Status != InteractionPending {
		return Receipt{}, Conflict("该交互已处理：" + req.Status)
	}
	if interactionExpiredAt(req, time.Now().UTC()) {
		markExpired(st, c, index, req)
		return Receipt{}, errInteractionExpired
	}
	sub, err := ValidateInteractionSubmission(req, Submission{Mode: cmd.AnswerMode, Answers: cmd.Answers, Others: cmd.Others, Comment: cmd.Comment, Text: cmd.Text})
	if err != nil {
		return Receipt{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	response := &InteractionResponse{OperationID: cmd.OperationID, Mode: sub.Mode, Answers: sub.Answers, Others: sub.Others, Comment: sub.Comment, Text: sub.Text, Delivery: DeliveryRecorded, AnsweredAt: now}
	r := Receipt{InteractionID: req.ID, InteractionVersion: req.Version, Delivery: DeliveryRecorded, ConversationID: c.ID}
	req.Status = InteractionAnswered
	req.StatusReason = ""
	req.Response = response
	c.Interactions[index] = req
	// Answers to several queued ordinary questions are recorded first. Only
	// when no blocking question is left does one aggregated user turn go to
	// the original session, so a dependent flow never resumes half-answered.
	if req.Source != SourceAgentUI {
		pending := pendingQuestionCountAcross(*st, c.ID, req.ID)
		if pending == 0 {
			// Reject an aggregate that could never be sent before it is stored.
			if text, count := composeInteractionBatch(*c); count > 0 && !validRequiredText(text, 5000) {
				return Receipt{}, Invalid("多个答复合起来过长，请精简后用聊天说明")
			}
		}
		switch {
		case pending > 0:
			response.Note = fmt.Sprintf("答复已记录；还有 %d 个问题待回答，全部完成后会一起发送给 AI", pending)
		case c.RunState == "running" || c.RunState == "starting":
			response.Note = "答复已记录；当前轮次结束后会发送给 AI"
		}
		if pending > 0 || c.RunState == "running" || c.RunState == "starting" {
			saveInteraction(st, c, index, req)
			return r, nil
		}
	}
	// Persist the answer before any dispatch intent so a crash cannot lose it.
	saveInteraction(st, c, index, req)
	if req.Source != SourceAgentUI {
		messageID, err := flushInteractionAnswers(st, c)
		if err != nil {
			return Receipt{}, err
		}
		r.CreatedID = messageID
	}
	return r, nil
}

// pendingQuestionCountAcross counts other still-answerable ordinary questions
// in the conversation. Native dialogs wait on their own upstream request and
// never hold back an ordinary answer batch.
func pendingQuestionCountAcross(st State, conversationID, exclude string) int {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return 0
	}
	now := time.Now().UTC()
	count := 0
	for _, r := range c.Interactions {
		if r.ID == exclude || r.Source != SourceAssistantQuestion || !r.Blocking || r.Status != InteractionPending || interactionExpiredAt(r, now) {
			continue
		}
		count++
	}
	return count
}

func submissionFromResponse(r InteractionRequest) Submission {
	sub := Submission{Mode: AnswerForm}
	if r.Response == nil {
		return sub
	}
	sub.Mode = r.Response.Mode
	sub.Answers = r.Response.Answers
	sub.Others = r.Response.Others
	sub.Comment = r.Response.Comment
	sub.Text = r.Response.Text
	return sub
}

// composeInteractionBatch renders every recorded-but-unsent ordinary answer
// as one user turn. Cancelled questions are never rendered as answers.
func composeInteractionBatch(c Conversation) (string, int) {
	var parts []string
	for _, r := range c.Interactions {
		if r.Source != SourceAssistantQuestion || r.Status != InteractionAnswered || r.Response == nil || r.Response.MessageID != "" {
			continue
		}
		parts = append(parts, ComposeInteractionAnswer(r, submissionFromResponse(r)))
	}
	if len(parts) == 0 {
		return "", 0
	}
	text := strings.Join(parts, "\n\n")
	if len(parts) > 1 {
		text = "以下是对之前几个问题的答复：\n\n" + text
	}
	return text, len(parts)
}

// flushInteractionAnswers sends one aggregated user turn carrying every
// recorded answer that has not reached the original session yet, and links
// those answers to the created round so delivery can track it.
func flushInteractionAnswers(st *State, c *Conversation) (string, error) {
	if c.RunState == "running" || c.RunState == "starting" {
		return "", Conflict("会话正在运行，请等待本轮结束后再继续")
	}
	if len(c.Messages) >= 2000 {
		return "", Invalid("会话消息已达上限")
	}
	text, _ := composeInteractionBatch(*c)
	targets := []int{}
	for i := range c.Interactions {
		r := c.Interactions[i]
		if r.Source == SourceAssistantQuestion && r.Status == InteractionAnswered && r.Response != nil && r.Response.MessageID == "" {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return "", nil
	}
	if !validRequiredText(text, 5000) {
		return "", Invalid("多个答复合起来过长，请精简后用聊天说明")
	}
	messageID := id()
	c.Messages = append(c.Messages, Message{ID: messageID, Role: "user", Text: text, Delivery: "pending"})
	c.RunState = "starting"
	c.Error = ""
	now := time.Now().UTC().Format(time.RFC3339)
	for _, i := range targets {
		c.Interactions[i].Response.MessageID = messageID
		c.Interactions[i].Response.FollowUpRoundID = messageID
		c.Interactions[i].Response.Delivery = DeliveryRecorded
		c.Interactions[i].Response.UpdatedAt = now
	}
	// The answer dispatch now owns the next turn; any dependency is pending
	// until that turn's round settles.
	if c.TaskID != "" {
		t := st.Tasks[c.TaskID]
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
	st.Conversations[c.ID] = *c
	return messageID, nil
}

func cancelInteraction(st *State, cmd Command) (Receipt, error) {
	req, c, index, err := locateInteraction(st, cmd)
	if err != nil {
		return Receipt{}, err
	}
	if err = interactionVersion(req, cmd); err != nil {
		return Receipt{}, err
	}
	if req.Status != InteractionPending {
		return Receipt{}, Conflict("该交互已处理：" + req.Status)
	}
	if !req.AllowCancel {
		return Receipt{}, Invalid("该请求不支持取消")
	}
	if _, err = ValidateInteractionSubmission(req, Submission{Mode: AnswerCancel}); err != nil {
		return Receipt{}, err
	}
	if interactionExpiredAt(req, time.Now().UTC()) {
		markExpired(st, c, index, req)
		return Receipt{}, errInteractionExpired
	}
	now := time.Now().UTC().Format(time.RFC3339)
	response := &InteractionResponse{OperationID: cmd.OperationID, Mode: AnswerCancel, AnsweredAt: now, Delivery: DeliveryRecorded}
	if req.Source != SourceAgentUI {
		// Nothing is waiting upstream: cancelling an ordinary question is a
		// local decision, never an AI-visible yes/no.
		response.Delivery = DeliveryAccepted
		response.Note = "已在本地取消本次询问，不会向 AI 发送同意或拒绝，也不会停止任务"
	}
	req.Status = InteractionCancelled
	req.StatusReason = ""
	req.Response = response
	saveInteraction(st, c, index, req)
	receipt := Receipt{InteractionID: req.ID, InteractionVersion: req.Version, Delivery: response.Delivery, ConversationID: c.ID}
	if req.Source != SourceAgentUI && pendingQuestionCountAcross(*st, c.ID, "") == 0 && c.RunState != "running" && c.RunState != "starting" {
		// Cancelling never invents an answer, but answers already recorded for
		// the other questions still have to reach the AI once nothing is left
		// to ask. A cancellation alone creates no turn.
		messageID, err := flushInteractionAnswers(st, c)
		if err != nil {
			return Receipt{}, err
		}
		receipt.CreatedID = messageID
	}
	return receipt, nil
}

func retryInteractionAnswer(st *State, cmd Command) (Receipt, error) {
	req, c, index, err := locateInteraction(st, cmd)
	if err != nil {
		return Receipt{}, err
	}
	if err = interactionVersion(req, cmd); err != nil {
		return Receipt{}, err
	}
	if req.Response == nil || (req.Status != InteractionAnswered && req.Status != InteractionCancelled) {
		return Receipt{}, Conflict("没有可重试的答复")
	}
	if req.Response.Delivery == DeliveryUncertain || req.Response.Delivery == DeliveryDispatching {
		return Receipt{}, Conflict("送达状态不确定，请先核对原会话；不会自动重发")
	}
	if req.Response.Delivery != DeliveryNotSent {
		return Receipt{}, Conflict("答复已送达或已记录，无需重试")
	}
	r := Receipt{InteractionID: req.ID, InteractionVersion: req.Version, ConversationID: c.ID}
	if req.Source == SourceAgentUI {
		if req.Status == InteractionExpired || req.Native == nil {
			return Receipt{}, Conflict("原生请求已失效，请让 AI 重新发起")
		}
		req.Response.Delivery = DeliveryRecorded
		req.Response.Note = "正在重试发送到原 pi 等待请求"
		req.Response.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		saveInteraction(st, c, index, req)
		r.Delivery = DeliveryRecorded
		return r, nil
	}
	if req.Response.MessageID == "" {
		return Receipt{}, Conflict("无法确认原答复消息，请用聊天补充")
	}
	messageIndex := -1
	for i := range c.Messages {
		if c.Messages[i].ID == req.Response.MessageID {
			messageIndex = i
		}
	}
	if messageIndex < 0 {
		return Receipt{}, Conflict("原答复消息已不存在，请用聊天补充")
	}
	if c.RunState == "running" || c.RunState == "starting" {
		return Receipt{}, Conflict("会话正在运行，请等待本轮结束后重试")
	}
	c.Messages[messageIndex].Delivery = "pending"
	c.RunState = "starting"
	c.Error = ""
	req.Response.Delivery = DeliveryRecorded
	req.Response.Note = "正在重试发送到原 pi 会话"
	req.Response.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if c.TaskID != "" {
		t := st.Tasks[c.TaskID]
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
	saveInteraction(st, c, index, req)
	r.CreatedID = req.Response.MessageID
	r.Delivery = DeliveryRecorded
	return r, nil
}

// DeliveryDependency resolves the round that really settles dependent
// automated work. A round that asked a blocking question is not settled by the
// answer row alone: the round created from the answers has to finish. A
// cancelled-only round stays waiting until an explicit later user turn
// finishes, so cancellation never silently releases development or delivery.
func DeliveryDependency(st State, taskID, roundID string) (string, bool, string, string) {
	t, ok := st.Tasks[taskID]
	if !ok {
		return roundID, false, "round_missing", "任务不存在"
	}
	c, ok := st.Conversations[t.ConversationID]
	if !ok {
		return roundID, false, "round_missing", "找不到任务会话"
	}
	current := roundID
	seen := map[string]bool{}
	now := time.Now().UTC()
	fromAnswer := false
	for hop := 0; hop < 12; hop++ {
		if current == "" || seen[current] {
			return current, false, "interaction_cycle", "交互依赖链异常，自动流程保持等待"
		}
		seen[current] = true
		index := -1
		for i := range c.Rounds {
			if c.Rounds[i].ID == current {
				index = i
				break
			}
		}
		if index < 0 {
			return current, false, "round_missing", "找不到周期对应轮次"
		}
		round := c.Rounds[index]
		if round.InteractionError != "" {
			return current, false, "interaction_invalid", "本轮交互结构无效，自动流程已暂停：" + round.InteractionError
		}
		if round.Phase == "finished" || round.Phase == "confirmed" {
			return current, true, "", ""
		}
		if round.Phase != "awaiting_user" {
			if fromAnswer {
				switch round.Phase {
				case "running", "starting":
					return current, false, "interaction_answering", "已收到答复，等待依赖轮次完成"
				case "failed", "interrupted":
					return current, false, "interaction_unanswered", "依赖轮次未完成，可重试发送答复或继续对话"
				}
			}
			return current, false, "", ""
		}
		blocks := []InteractionRequest{}
		for _, req := range c.Interactions {
			if req.RoundID == current && req.Blocking {
				blocks = append(blocks, req)
			}
		}
		if len(blocks) == 0 {
			return current, false, "", ""
		}
		pending, answered, followUp, cancelled := false, false, "", false
		for _, req := range blocks {
			switch req.Status {
			case InteractionPending:
				if interactionExpiredAt(req, now) {
					cancelled = true
					continue
				}
				pending = true
			case InteractionAnswered:
				if req.Response == nil || req.Response.FollowUpRoundID == "" {
					return current, false, "interaction_pending", "已记录的答复尚未形成依赖轮次，自动流程保持等待"
				}
				answered = true
				followUp = req.Response.FollowUpRoundID
			default:
				cancelled = true
			}
		}
		if pending {
			return current, false, "interaction_pending", "还有待你处理的交互问题，回答后才能继续"
		}
		if answered {
			current = followUp
			fromAnswer = true
			continue
		}
		if cancelled {
			if next, ok := adoptedRound(c, round); ok {
				current = next
				fromAnswer = true
				continue
			}
			return current, false, "interaction_unanswered", "本次询问已取消，AI 未收到答案；发送新的说明或重新发起后才能继续"
		}
		return current, false, "interaction_unanswered", "询问没有形成有效答复轮次"
	}
	return current, false, "interaction_cycle", "交互依赖链过深，自动流程保持等待"
}

// adoptedRound is the explicit user path after a cancellation: the latest
// round appended after the waiting round, created by a new user turn. Its
// phase decides whether it is still running (AI-active), finished (settled) or
// failed (waiting for a retry); round order is authoritative because
// timestamps only have second granularity.
func adoptedRound(c Conversation, round Round) (string, bool) {
	index := -1
	for i := range c.Rounds {
		if c.Rounds[i].ID == round.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return "", false
	}
	best := ""
	for _, candidate := range c.Rounds[index+1:] {
		best = candidate.ID
	}
	return best, best != ""
}

func hasRecordedAnswerAwaitingBatch(c Conversation) bool {
	for _, r := range c.Interactions {
		if r.Source == SourceAssistantQuestion && r.Status == InteractionAnswered && r.Response != nil && r.Response.MessageID == "" {
			return true
		}
	}
	return false
}

func hasUnsentInteractionAnswer(st State, conversationID string) bool {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return false
	}
	unsent := map[string]bool{}
	for _, r := range c.Interactions {
		if r.Response == nil || r.Response.MessageID == "" {
			continue
		}
		unsent[r.Response.MessageID] = true
	}
	for _, m := range c.Messages {
		// A never-accepted message must not be bypassed by a new chat turn; an
		// uncertain window is handled like any interrupted send (the user may
		// continue, and delivery stays waiting for a real settlement).
		if unsent[m.ID] && (m.Delivery == "pending" || m.Delivery == "failed" || m.Delivery == "not_sent") {
			return true
		}
	}
	return false
}

// SetInteractionDelivery persists one transport observation for a stored
// answer. It is the only place delivery status is written after the answer.
func SetInteractionDelivery(st *State, conversationID, requestID, delivery, note string) bool {
	req, c, index, err := interactionRequest(st, conversationID, requestID)
	if err != nil || req.Response == nil || !validDelivery(delivery) {
		return false
	}
	req.Response.Delivery = delivery
	if note != "" {
		req.Response.Note = note
	}
	req.Response.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	saveInteraction(st, c, index, *req)
	return true
}

// SyncInteractionDeliveryFromMessage maps the conversation message transport
// state onto the bound answer, so snapshots never show "accepted" while the
// message is still pending and vice versa.
func SyncInteractionDeliveryFromMessage(st *State, conversationID, messageID string) bool {
	c, ok := st.Conversations[conversationID]
	if !ok || messageID == "" {
		return false
	}
	delivery := ""
	for _, m := range c.Messages {
		if m.ID != messageID {
			continue
		}
		switch m.Delivery {
		case "accepted", "complete":
			delivery = DeliveryAccepted
		case "uncertain":
			delivery = DeliveryUncertain
		case "pending":
			delivery = DeliveryDispatching
		default:
			delivery = DeliveryNotSent
		}
		break
	}
	if delivery == "" {
		return false
	}
	changed := false
	for i := range c.Interactions {
		req := &c.Interactions[i]
		if req.Response == nil || req.Response.MessageID != messageID {
			continue
		}
		if req.Response.Delivery != delivery {
			req.Response.Delivery = delivery
			req.Response.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
		}
	}
	if changed {
		st.Conversations[c.ID] = c
	}
	return changed
}

// FlushInteractionAnswers is the reliable-boundary dispatch of answers that
// were recorded while a run was active. It is idempotent: only answers without
// a message are consumed, and only an idle conversation with no open ordinary
// question is eligible.
func (s *Store) FlushInteractionAnswers(conversationID string) (string, error) {
	s.mu.Lock()
	hook := s.userTurnHook
	var turnTask string
	// Runs after the store lock is released, like the command path.
	defer func() {
		if turnTask != "" && hook != nil {
			hook(turnTask, conversationID)
		}
	}()
	defer s.mu.Unlock()
	if s.closed {
		return "", errors.New("store closed")
	}
	c, ok := s.state.Conversations[conversationID]
	if !ok {
		return "", Missing()
	}
	if c.RunState != "idle" || pendingQuestionCountAcross(s.state, conversationID, "") > 0 {
		return "", nil
	}
	next := clone(s.state)
	nc := next.Conversations[conversationID]
	messageID, err := flushInteractionAnswers(&next, &nc)
	if err != nil || messageID == "" {
		return "", err
	}
	// The deferred turn needs the same round binding as a normal answer so
	// delivery can follow it to settlement.
	BeginRound(&next, conversationID, messageID)
	if err := s.persist(next, "", "", Receipt{}); err != nil {
		return "", err
	}
	if nc.TaskID != "" {
		turnTask = nc.TaskID
	}
	return messageID, nil
}

func (s *Store) Interaction(conversationID, requestID string) (*InteractionRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.state.Conversations[conversationID]
	if !ok {
		return nil, Missing()
	}
	index := interactionIndex(c, requestID)
	if index < 0 {
		return nil, Missing()
	}
	req := c.Interactions[index]
	return &req, nil
}

func (s *Store) InteractionGate(taskID, roundID string) (code, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return InteractionGate(s.state, taskID, roundID)
}

func (s *Store) taskInteractionGateLocked(taskID string) (InteractionRequest, bool) {
	t, ok := s.state.Tasks[taskID]
	if !ok {
		return InteractionRequest{}, false
	}
	return PendingBlockingInteraction(s.state, t.ConversationID)
}

func (s *Store) TaskBlockingInteraction(taskID string) (InteractionRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.state.Tasks[taskID]
	if !ok {
		return InteractionRequest{}, false
	}
	return PendingBlockingInteraction(s.state, t.ConversationID)
}

// IsInteractionCommand reports the three explicit interaction commands; they
// are bound to requestId/requestVersion rather than the global revision.
func IsInteractionCommand(typ string) bool {
	switch typ {
	case "answer_interaction", "cancel_interaction", "retry_interaction_answer":
		return true
	}
	return false
}

// NormalizeInteractionsOnRestart never recreates upstream state. Ordinary
// questions survive a restart and can be answered into the restored session;
// native dialogs cannot, so pending ones expire. Recorded answers are mapped
// to the durable message evidence; an unprovable native write becomes
// uncertain instead of claiming delivery.
func NormalizeInteractionsOnRestart(st *State, conversationID string) {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return
	}
	messageDelivery := func(messageID string) (string, bool) {
		for _, m := range c.Messages {
			if m.ID == messageID {
				return m.Delivery, true
			}
		}
		return "", false
	}
	for i := range c.Interactions {
		req := &c.Interactions[i]
		if req.Source == SourceAgentUI && req.Status == InteractionPending {
			req.Status = InteractionExpired
			req.StatusReason = "服务已重启，原 pi 等待请求无法恢复；请让 AI 重新发起"
		}
		if req.Response == nil {
			continue
		}
		switch req.Response.Delivery {
		case DeliveryRecorded, DeliveryDispatching:
			switch {
			case req.Source == SourceAgentUI:
				req.Response.Delivery = DeliveryUncertain
				req.Response.Note = "服务重启，原生答复是否已送达无法证明；不会自动重发"
			case req.Response.MessageID != "":
				if delivery, found := messageDelivery(req.Response.MessageID); found {
					switch delivery {
					case "accepted", "complete":
						req.Response.Delivery = DeliveryAccepted
					case "uncertain":
						req.Response.Delivery = DeliveryUncertain
						req.Response.Note = "服务重启，答复发送状态需核对，不会自动重发"
					case "pending":
						req.Response.Delivery = DeliveryUncertain
						req.Response.Note = "服务重启前递送状态未确认；不会自动重发"
					default:
						req.Response.Delivery = DeliveryNotSent
						req.Response.Note = "答复已保存，尚未发送到 AI；可明确重试发送"
					}
				} else {
					req.Response.Delivery = DeliveryUncertain
					req.Response.Note = "服务重启，原答复消息缺失；不会自动重发"
				}
			}
		}
	}
	st.Conversations[c.ID] = c
}

// SyncConversationInteractionsFromMessages re-derives every linked answer's
// transport state from its durable message.
func SyncConversationInteractionsFromMessages(st *State, conversationID string) {
	c, ok := st.Conversations[conversationID]
	if !ok {
		return
	}
	ids := []string{}
	for _, r := range c.Interactions {
		if r.Response != nil && r.Response.MessageID != "" {
			ids = append(ids, r.Response.MessageID)
		}
	}
	for _, id := range ids {
		SyncInteractionDeliveryFromMessage(st, conversationID, id)
	}
}
