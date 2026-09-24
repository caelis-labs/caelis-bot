package api

import "context"

// Setup is a user-only management surface. It is never exposed to model tools.
// Secrets are write-only arguments and must not enter settings or operation journals.
type SetupRequest struct {
	Settings RuntimeSettings `json:"settings"`
	Action   string          `json:"action"`
	Provider string          `json:"provider"`
	BaseURL  string          `json:"baseUrl"`
	Model    string          `json:"model"`
	APIKey   string          `json:"apiKey"`
}
type SetupChoice struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	NoAuth  bool   `json:"noAuth"`
	Current bool   `json:"current"`
}
type SetupState struct {
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
	Active     string `json:"active"`
	Pending    string `json:"pending"`
	Onboarding bool   `json:"onboarding"`
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
