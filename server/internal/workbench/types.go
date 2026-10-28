// Package workbench owns durable product state, independent of CLI and HTTP.
package workbench

import "fmt"

type Profile struct {
	CredentialSource string `json:"credentialSource,omitempty"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	Thinking         string `json:"thinking"`
}
type Project struct {
	MainSeen           bool   `json:"mainSeen,omitempty"`
	DefaultBranch      string `json:"defaultBranch,omitempty"`
	MainBranchOverride string `json:"mainBranchOverride,omitempty"`
	CommitName         string `json:"commitName,omitempty"`
	CommitEmail        string `json:"commitEmail,omitempty"`
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Path               string `json:"path"`
	ConversationID     string `json:"conversationId"`
}
type Topic struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}
type Task struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	TopicID        string  `json:"topicId"`
	Title          string  `json:"title"`
	Request        string  `json:"request"`
	Status         string  `json:"status"`
	ConversationID string  `json:"conversationId"`
	Profile        Profile `json:"profile"`
	ExecutionID    string  `json:"executionId,omitempty"`
}
type ToolRecord struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Result string `json:"result,omitempty"`
	Failed bool   `json:"failed"`
}
type Execution struct {
	ID             string       `json:"id"`
	TaskID         string       `json:"taskId"`
	ConversationID string       `json:"conversationId"`
	Workspace      string       `json:"workspace,omitempty"`
	Branch         string       `json:"branch,omitempty"`
	BaseSHA        string       `json:"baseSHA,omitempty"`
	Phase          string       `json:"phase"`
	Error          string       `json:"error,omitempty"`
	Summary        string       `json:"summary,omitempty"`
	StartedAt      string       `json:"startedAt"`
	FinishedAt     string       `json:"finishedAt,omitempty"`
	Profile        Profile      `json:"profile"`
	Tools          []ToolRecord `json:"tools"`
}
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int    `json:"size"`
}
type Message struct {
	Attachments []Attachment `json:"attachments,omitempty"`
	Kind        string       `json:"kind,omitempty"`
	DisplayText *string      `json:"displayText,omitempty"`
	ID          string       `json:"id"`
	Role        string       `json:"role"`
	Text        string       `json:"text"`
	Delivery    string       `json:"delivery"`
}
type Conversation struct {
	Rounds      []Round   `json:"rounds,omitempty"`
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	TaskID      string    `json:"taskId,omitempty"`
	SessionID   string    `json:"sessionId"`
	SessionFile string    `json:"sessionFile,omitempty"`
	RunState    string    `json:"runState"`
	Error       string    `json:"error,omitempty"`
	Profile     Profile   `json:"profile"`
	Messages    []Message `json:"messages"`
	// Interactions are the authoritative user-interaction requests of this
	// conversation. report.question stays readable history, never a live form.
	Interactions []InteractionRequest `json:"interactions,omitempty"`
	// PendingProfile is the requested model for the NEXT round; it never
	// interrupts the running request and never rewrites the template.
	PendingProfile *Profile `json:"pendingProfile,omitempty"`
}
type State struct {
	Deliveries        map[string]DeliverySummary       `json:"deliveries"`
	DeliveryPolicies  map[string]DeliveryPolicySummary `json:"deliveryPolicies"`
	ProfileConfigured bool                             `json:"profileConfigured,omitempty"`
	StructureRevision uint64                           `json:"structureRevision"`
	TaskGit           map[string]GitStatus             `json:"taskGit"`
	Merges            map[string]MergeOperation        `json:"merges"`
	Revision          uint64                           `json:"revision"`
	Projects          map[string]Project               `json:"projects"`
	Topics            []Topic                          `json:"topics"`
	Tasks             map[string]Task                  `json:"tasks"`
	Conversations     map[string]Conversation          `json:"conversations"`
	Profile           Profile                          `json:"profile"`
	Executions        map[string]Execution             `json:"executions"`
}
type Command struct {
	Attachments        []Attachment      `json:"attachments,omitempty"`
	RoundID            string            `json:"roundId,omitempty"`
	Provider           string            `json:"provider,omitempty"`
	Model              string            `json:"model,omitempty"`
	Thinking           string            `json:"thinking,omitempty"`
	StructureRevision  *uint64           `json:"structureRevision,omitempty"`
	MainBranchOverride string            `json:"mainBranchOverride,omitempty"`
	CommitName         string            `json:"commitName,omitempty"`
	CommitEmail        string            `json:"commitEmail,omitempty"`
	Answers            map[string]any    `json:"answers,omitempty"`
	Others             map[string]string `json:"others,omitempty"`
	OperationID        string            `json:"operationId"`
	Revision           uint64            `json:"revision"`
	Type               string            `json:"type"`
	ProjectID          string            `json:"projectId,omitempty"`
	TopicID            string            `json:"topicId,omitempty"`
	TaskID             string            `json:"taskId,omitempty"`
	ConversationID     string            `json:"conversationId,omitempty"`
	Name               string            `json:"name,omitempty"`
	Path               string            `json:"path,omitempty"`
	InitializeGit      string            `json:"initializeGit,omitempty"`
	RemoteURL          string            `json:"remoteURL,omitempty"`
	Title              string            `json:"title,omitempty"`
	Request            string            `json:"request,omitempty"`
	Text               string            `json:"text,omitempty"`
	RequestID          string            `json:"requestId,omitempty"`
	AnswerMode         string            `json:"answerMode,omitempty"`
	Comment            string            `json:"comment,omitempty"`
	RequestVersion     int               `json:"requestVersion,omitempty"`
}
type Receipt struct {
	DeliveryID         string `json:"deliveryId,omitempty"`
	Replay             bool   `json:"-"`
	Revision           uint64 `json:"revision"`
	CreatedID          string `json:"createdId,omitempty"`
	ConversationID     string `json:"conversationId,omitempty"`
	Notice             string `json:"notice,omitempty"`
	Type               string `json:"type"`
	InteractionID      string `json:"interactionId,omitempty"`
	InteractionVersion int    `json:"interactionVersion,omitempty"`
	Delivery           string `json:"delivery,omitempty"`
}
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string      { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func Invalid(message string) error  { return &Error{"invalid_input", message} }
func Conflict(message string) error { return &Error{"conflict", message} }
func Missing() error                { return &Error{"not_found", "目标不存在或不属于当前项目"} }
