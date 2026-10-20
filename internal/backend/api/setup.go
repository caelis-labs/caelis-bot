package api

import "context"

// ModelSelectionState distinguishes a user's saved preference from confirmed
// Runtime application. It contains no native error, credentials or task IDs.
type ModelSelectionState struct {
	Scope     string                `json:"scope"`
	Runtime   string                `json:"runtime"`
	Selection WorkExecutionSettings `json:"selection"`
	Revision  uint64                `json:"revision"`
	State     string                `json:"state"` // saving, pending, applying, recovering, applied
}

// Setup is a user-only management surface. It is never exposed to model tools.
// Secrets are write-only arguments and must not enter settings or operation journals.
type SetupRequest struct {
	LoginID  string          `json:"loginId,omitempty"` // Product attempt fence, never an OAuth secret.
	Settings RuntimeSettings `json:"settings"`
	Action   string          `json:"action"`
	Provider string          `json:"provider"`
	BaseURL  string          `json:"baseUrl"`
	Model    string          `json:"model"`
	APIKey   string          `json:"apiKey"`
}
type SetupChoice struct {
	Parameters *ModelOption `json:"parameters,omitempty"`
	Value      string       `json:"value"`
	Label      string       `json:"label"`
	NoAuth     bool         `json:"noAuth"`
	Current    bool         `json:"current"`
}

// SetupLogin is an ephemeral user-only challenge. Never persist or broadcast it.
// ExpiresAt is Bot's local display/cancellation deadline, not an issuer claim.
type SetupLogin struct {
	ID              string `json:"id"`
	State           string `json:"state"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	ExpiresAt       string `json:"expiresAt,omitempty"`
}
type SetupState struct {
	Login *SetupLogin `json:"login,omitempty"`

	ServiceUpdateAvailable bool            `json:"serviceUpdateAvailable"` // Installed CLI is newer than the running Host.
	ServiceVersion         string          `json:"serviceVersion"`
	ServiceState           string          `json:"serviceState"` // running, unknown (Caelis only)
	SelectedModel          string          `json:"selectedModel"`
	Settings               RuntimeSettings `json:"settings"`
	Installation           RuntimeStatus   `json:"installation"`
	State                  string          `json:"state"` // missing, service, unavailable, incompatible, login, models, ready
	Message                string          `json:"message"`
	Models                 []SetupChoice   `json:"models"`
	LoginPending           bool            `json:"loginPending"`
	AccountType            string          `json:"accountType"`
}
type SetupOverview struct {
	Active           string `json:"active"`
	HasRuntimeChoice bool   `json:"hasRuntimeChoice"`
	Pending          string `json:"pending"`
	Onboarding       bool   `json:"onboarding"`
}
type SetupController interface {
	Overview() SetupOverview
	Profile(string) (RuntimeSettings, error)
	Inspect(context.Context, RuntimeSettings) (SetupState, error)
	Catalog(context.Context, SetupRequest) ([]SetupChoice, error)
	Apply(context.Context, SetupRequest) (SetupState, error)
	Activate(context.Context, RuntimeSettings) error
	Dismiss() error
}

// SetupInstallProgress reports measured download bytes, never an estimated completion.
type SetupInstallProgress struct {
	Phase          string `json:"phase"`
	Downloaded     int64  `json:"downloaded"`
	Total          int64  `json:"total"`
	BytesPerSecond int64  `json:"bytesPerSecond"`
}
