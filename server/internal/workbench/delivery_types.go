package workbench

import "time"

// Delivery policy contains credential references, never credentials.
type DeliveryCheck struct {
	ID             string   `json:"id"`
	Argv           []string `json:"argv"`
	Dir            string   `json:"dir"`
	TimeoutSeconds int64    `json:"timeoutSeconds"`
}
type DeliveryWorkflow struct {
	ID   int64    `json:"id"`
	Jobs []string `json:"jobs"`
}
type DeliveryRepository struct {
	ID         int64  `json:"id"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	MainBranch string `json:"mainBranch"`
}
type ProjectDeliveryPolicy struct {
	ProjectID           string             `json:"projectId"`
	Version             uint64             `json:"version"`
	Enabled             bool               `json:"enabled"`
	RequireAcceptance   bool               `json:"requireAcceptance"`
	AllowCreateMain     bool               `json:"allowCreateMain"`
	Repository          DeliveryRepository `json:"repository"`
	Checks              []DeliveryCheck    `json:"checks"`
	TaskWorkflows       []DeliveryWorkflow `json:"taskWorkflows"`
	MainWorkflows       []DeliveryWorkflow `json:"mainWorkflows"`
	MaxRepairs          int                `json:"maxRepairs"`
	ActiveSeconds       int64              `json:"activeSeconds"`
	TriggerSeconds      int64              `json:"triggerSeconds"`
	CISeconds           int64              `json:"ciSeconds"`
	GitCredentialRef    string             `json:"gitCredentialRef"`
	APICredentialRef    string             `json:"apiCredentialRef"`
	AuthorizationDigest string             `json:"authorizationDigest"`
	Actor               string             `json:"actor"`
	AuthorizedAt        time.Time          `json:"authorizedAt"`
	RevokedAt           time.Time          `json:"revokedAt"`
}
type Delivery struct {
	ID                   string                `json:"id"`
	ProjectID            string                `json:"projectId"`
	TaskID               string                `json:"taskId"`
	IntentID             string                `json:"intentId"`
	Phase                string                `json:"phase"`
	Cycle                uint64                `json:"cycle"`
	Version              uint64                `json:"version"`
	Policy               ProjectDeliveryPolicy `json:"policy"`
	CommonDir            string                `json:"commonDir"`
	CandidateSHA         string                `json:"candidateSHA"`
	CandidateTree        string                `json:"candidateTree"`
	Fingerprint          string                `json:"fingerprint"`
	ProtectedFingerprint string                `json:"protectedFingerprint"`
	BaseMainSHA          string                `json:"baseMainSHA"`
	LocalMainSHA         string                `json:"localMainSHA"`
	RemoteMainSHA        string                `json:"remoteMainSHA"`
	RoundID              string                `json:"roundId"`
	AcceptanceToken      string                `json:"acceptanceToken"`
	AcceptedSHA          string                `json:"acceptedSHA"`
	BlockCode            string                `json:"blockCode"`
	BlockReason          string                `json:"blockReason"`
	// WaitingCode/WaitingReason keep the cycle alive while a user answer is
	// outstanding. They are never terminal: the same cycle continues when the
	// dependency round settles.
	WaitingCode   string `json:"waitingCode,omitempty"`
	WaitingReason string `json:"waitingReason,omitempty"`
	// WaitingSince is the start of the current user-answer wait.
	// WaitExcludedMillis accumulates completed waits (including service
	// downtime) so the activation budget never charges time in which no AI ran.
	// Both are persisted: restarts and repeated questions cannot reset budget.
	WaitingSince       time.Time `json:"waitingSince,omitempty"`
	WaitExcludedMillis int64     `json:"waitExcludedMillis,omitempty"`
	RepairsUsed        int       `json:"repairsUsed"`
	RepairsLimit       int       `json:"repairsLimit"`
	ActiveMillis       int64     `json:"activeMillis"`
	ActiveLimitSeconds int64     `json:"activeLimitSeconds"`
	ActiveSince        time.Time `json:"activeSince"`
	TriggerDeadline    time.Time `json:"triggerDeadline"`
	CIDeadline         time.Time `json:"ciDeadline"`
	StartedAt          time.Time `json:"startedAt"`
	FinishedAt         time.Time `json:"finishedAt"`
	HoldsTurn          bool      `json:"holdsTurn"`
}
type DeliveryStep struct {
	ID               string    `json:"id"`
	DeliveryID       string    `json:"deliveryId"`
	Kind             string    `json:"kind"`
	InputFingerprint string    `json:"inputFingerprint"`
	Phase            string    `json:"phase"`
	BeforeSHA        string    `json:"beforeSHA"`
	AfterSHA         string    `json:"afterSHA"`
	Tree             string    `json:"tree"`
	Ref              string    `json:"ref"`
	Summary          string    `json:"summary"`
	BootID           string    `json:"bootId"`
	ProcessGroup     int       `json:"processGroup"`
	ExitCode         int       `json:"exitCode"`
	ExitKnown        bool      `json:"exitKnown"`
	Truncated        bool      `json:"truncated"`
	Sequence         uint64    `json:"sequence"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
}
type DeliveryCI struct {
	DeliveryID   string            `json:"deliveryId"`
	Stage        string            `json:"stage"`
	Branch       string            `json:"branch"`
	SHA          string            `json:"sha"`
	Event        string            `json:"event"`
	Status       string            `json:"status"`
	Conclusion   string            `json:"conclusion"`
	URL          string            `json:"url"`
	RepositoryID int64             `json:"repositoryId"`
	WorkflowID   int64             `json:"workflowId"`
	RunID        int64             `json:"runId"`
	Attempt      int               `json:"attempt"`
	Jobs         map[string]string `json:"jobs"`
	ObservedAt   time.Time         `json:"observedAt"`
}
type DeliveryRepair struct {
	ID                 string  `json:"id"`
	DeliveryID         string  `json:"deliveryId"`
	FailureFingerprint string  `json:"failureFingerprint"`
	FailedSHA          string  `json:"failedSHA"`
	Stage              string  `json:"stage"`
	Prompt             string  `json:"-"`
	Phase              string  `json:"phase"`
	MessageID          string  `json:"messageId"`
	Number             int     `json:"number"`
	Profile            Profile `json:"profile"`
}

// Summary is deliberately not an embedded Delivery: additions to private state
// must not silently become public snapshot fields.
type DeliverySummary struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"projectId"`
	TaskID            string         `json:"taskId"`
	Phase             string         `json:"phase"`
	Version           uint64         `json:"version"`
	Cycle             uint64         `json:"cycle"`
	CandidateSHA      string         `json:"candidateSHA"`
	LocalMainSHA      string         `json:"localMainSHA"`
	RemoteMainSHA     string         `json:"remoteMainSHA"`
	RequireAcceptance bool           `json:"requireAcceptance"`
	AcceptanceToken   string         `json:"acceptanceToken,omitempty"`
	AcceptedSHA       string         `json:"acceptedSHA,omitempty"`
	RepairsUsed       int            `json:"repairsUsed"`
	RepairsLimit      int            `json:"repairsLimit"`
	BlockCode         string         `json:"blockCode,omitempty"`
	BlockReason       string         `json:"blockReason,omitempty"`
	WaitingCode       string         `json:"waitingCode,omitempty"`
	WaitingReason     string         `json:"waitingReason,omitempty"`
	HoldsTurn         bool           `json:"holdsTurn"`
	Steps             []DeliveryStep `json:"steps"`
	CI                []DeliveryCI   `json:"ci"`
}
type DeliveryPolicySummary struct {
	Version           uint64             `json:"version"`
	Enabled           bool               `json:"enabled"`
	RequireAcceptance bool               `json:"requireAcceptance"`
	Repository        DeliveryRepository `json:"repository"`
}
type DeliveryStepPage struct {
	Items []DeliveryStep `json:"items"`
	Next  uint64         `json:"next"`
}

func DeliveryActive(phase string) bool {
	switch phase {
	case "queued", "developing", "local_checking", "committing", "pushing_task", "task_ci", "preparing_integration", "awaiting_acceptance", "integrating", "integration_checking", "pushing_main", "main_ci", "repairing", "blocked", "paused", "needs_check":
		return true
	}
	return false
}
