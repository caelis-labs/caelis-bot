// Package api is the Go authority for the host-to-renderer product projection.
// Native protocol payloads and paths used to dispatch work never come from the UI.
package api

import "context"

// SilentReminder is the exact final response for a skipped scheduled activation.
const SilentReminder = "[[CAELIS_REMINDER_SKIP]]"

type Snapshot struct {
	Scheduled        bool        `json:"scheduled"`
	Quiet            bool        `json:"quiet"`
	BotStatus        string      `json:"botStatus"`
	HasEarlier       bool        `json:"hasEarlier"`
	CurrentTurn      string      `json:"currentTurn"`
	PreviewKey       string      `json:"previewKey"`
	PreviewDismissed bool        `json:"previewDismissed"`
	Revision         uint64      `json:"revision"`
	Connection       string      `json:"connection"`
	ConnectionIssue  string      `json:"connectionIssue"`
	Phase            string      `json:"phase"`
	Message          string      `json:"message"`
	CanSend          bool        `json:"canSend"`
	CanSteer         bool        `json:"canSteer"`
	CanInterrupt     bool        `json:"canInterrupt"`
	Items            []Item      `json:"items"`
	Approvals        []Approval  `json:"approvals"`
	Reviews          []Review    `json:"reviews"`
	References       []Reference `json:"references"`
	LoginPending     bool        `json:"loginPending"`
	LastReceipt      Receipt     `json:"lastReceipt"`
}

type ChatUpdate struct {
	Changed  bool     `json:"changed"`
	Snapshot Snapshot `json:"snapshot"`
}
type AttachmentStorage struct {
	Files         int    `json:"files"`
	Bytes         int64  `json:"bytes"`
	EligibleFiles int    `json:"eligibleFiles"`
	EligibleBytes int64  `json:"eligibleBytes"`
	CanClean      bool   `json:"canClean"`
	Notice        string `json:"notice"`
}

// Review is a native automatic-review fact, never an actionable approval.
type Review struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Action    string `json:"action"`
	Rationale string `json:"rationale"`
}
type Item struct {
	TurnKey   string     `json:"turnKey"`
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Text      string     `json:"text"`
	Status    string     `json:"status"`
	Details   string     `json:"details"`
	Artifacts []Artifact `json:"artifacts"`
}
type Artifact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// LabelKey is set only for Bot-owned copy, never provider-supplied options.
	LabelKey string `json:"labelKey"`
	Scope    string `json:"scope"`
	Details  string `json:"details"`
}
type Question struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Secret   bool     `json:"secret"`
	Required bool     `json:"required"`
	Multiple bool     `json:"multiple"`
	Type     string   `json:"type"`
	Options  []Choice `json:"options"`
}

// ApprovalSection keeps explanatory labels separate from native permission payloads.
type ApprovalSection struct {
	TitleKey string `json:"titleKey"`
	Text     string `json:"text"`
}

type Approval struct {
	// Presentation keys never participate in approval identity or decisions.
	TitleKey    string            `json:"titleKey"`
	NoticeKey   string            `json:"noticeKey"`
	TaskTitle   string            `json:"taskTitle"`
	Sections    []ApprovalSection `json:"sections"`
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Action      string            `json:"action"`
	Target      string            `json:"target"`
	Description string            `json:"description"`
	Details     string            `json:"details"`
	Status      string            `json:"status"`
	Choices     []Choice          `json:"choices"`
	Questions   []Question        `json:"questions"`
	URL         string            `json:"url"`
}
type Reference struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}
type Draft struct {
	Notice       string   `json:"notice"`
	Revision     uint64   `json:"revision"`
	Text         string   `json:"text"`
	ReferenceIDs []string `json:"referenceIds"`
}
type RuntimeSettings struct {
	Runtime     string `json:"runtime"`
	CLIPath     string `json:"cliPath"`
	CaelisStore string `json:"caelisStore,omitempty"`
}
type RuntimeCheck struct {
	Saved     bool   `json:"saved"`
	Connected bool   `json:"connected"`
	Message   string `json:"message"`
}
type Submission struct {
	// Scheduled is host-only presentation provenance, not an authorization grant.
	Scheduled    bool     `json:"-"`
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	FileIDs      []string `json:"fileIds"`
	ReferenceIDs []string `json:"referenceIds"`
}
type Receipt struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"` // accepted, rejected or unknown; never inferred from prose.
	Message string `json:"message"`
}
type Decision struct {
	ID      string              `json:"id"`
	Choice  string              `json:"choice"`
	Answers map[string][]string `json:"answers"`
}

// InputFile is host-only. UI supplies opaque IDs resolved by the native selector.
type InputFile struct{ Name, Path string }
type Engine interface {
	Connect(context.Context) error
	Snapshot() Snapshot
	Submit(context.Context, Submission, []InputFile) (Receipt, error)
	Interrupt(context.Context) error
	Decide(context.Context, Decision) error
	Close(context.Context) error
}

// ExecutionSettings applies at the provider's next supported request boundary.
// Already-issued model requests and native approval targets are never rewritten.
// Empty Model inherits the current provider's configured default.
type ExecutionSettings struct {
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	ServiceTier  string `json:"serviceTier"`
	ApprovalMode string `json:"approvalMode"`
}

// WorkExecutionSettings selects only the model for newly delegated work.
// Empty Model means Runtime default, then Bot fallback. Permissions are separate.
type WorkExecutionSettings struct {
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	ServiceTier string `json:"serviceTier"`
}
type ModelOption struct {
	Model         string        `json:"model"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Default       bool          `json:"default"`
	DefaultEffort string        `json:"defaultEffort"`
	Efforts       []string      `json:"efforts"`
	ServiceTiers  []ServiceTier `json:"serviceTiers"`
}
type ServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type RuntimeStatus struct {
	LatestVersion string `json:"latestVersion"`
	UpdateState   string `json:"updateState"` // available, current, or empty (not checked)
	Installed     bool   `json:"installed"`
	Path          string `json:"path"`
	Version       string `json:"version"`
	Message       string `json:"message"`
}
