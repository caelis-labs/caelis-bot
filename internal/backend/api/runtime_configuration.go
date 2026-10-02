package api

import "context"

// RuntimeConfiguration is a user-owned projection of shared Host configuration.
// Team models use native profile IDs; main/conversation models use public selectors.
type RuntimeConfiguration struct {
	Revision       string                   `json:"revision"`
	Main           WorkExecutionSettings    `json:"main"`
	Models         []ModelOption            `json:"models"`
	Team           RuntimeTeam              `json:"team"`
	Connections    []RuntimeConnectionGroup `json:"connections"`
	OAuthAvailable bool                     `json:"oauthAvailable"`
}
type RuntimeTeam struct {
	Available bool             `json:"available"`
	Reason    string           `json:"reason"`
	Revision  string           `json:"revision"`
	Roles     []RuntimeRole    `json:"roles"`
	Sets      []RuntimeTeamSet `json:"sets"`
	ActiveSet string           `json:"activeSet"`
	Models    []ModelOption    `json:"models"`
}
type RuntimeRole struct {
	ID          string                `json:"id"`
	ModelIDs    []string              `json:"modelIds"`
	Description string                `json:"description"`
	System      bool                  `json:"system"`
	Custom      bool                  `json:"custom"`
	Selection   WorkExecutionSettings `json:"selection"`
	Inherited   bool                  `json:"inherited"`
	Problem     string                `json:"problem"`
}
type RuntimeTeamSet struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Problem   string `json:"problem"`
}
type RuntimeConnectionGroup struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Kind   string                   `json:"kind"`
	Detail string                   `json:"detail"`
	Models []RuntimeConnectionModel `json:"models"`
}
type RuntimeConnectionModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Uses        []string `json:"uses"`
	Unavailable bool     `json:"unavailable"`
}

// RuntimeConfigurationChange names a semantic operation, never a URL or command.
// ExpectedRevision is the revision displayed when the user began editing.
type RuntimeConfigurationChange struct {
	Action           string                `json:"action"`
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	Selection        WorkExecutionSettings `json:"selection"`
	ExpectedRevision string                `json:"expectedRevision"`
}
type RuntimeMutationResult struct {
	OperationID string `json:"operationId"`
	Outcome     string `json:"outcome"`
	Message     string `json:"message"`
}
type RuntimeConnectChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Custom      bool   `json:"custom"`
}
type RuntimeConnectionCatalog struct {
	Choices     []RuntimeConnectChoice `json:"choices"`
	Unavailable string                 `json:"unavailable"`
}
type RuntimeConnectionInput struct {
	Settings            *RuntimeSettings `json:"settings"`
	Kind                string           `json:"kind"`
	Choice              string           `json:"choice"`
	Command             string           `json:"command"`
	BaseURL             string           `json:"baseUrl"`
	Model               string           `json:"model"`
	APIKey              string           `json:"apiKey"`
	ContextWindowTokens int              `json:"contextWindowTokens"`
	MaxOutputTokens     int              `json:"maxOutputTokens"`
	ImageInput          *bool            `json:"imageInput"`
	ReasoningLevels     []string         `json:"reasoningLevels"`
}
type RuntimeFlowInput struct {
	Destination string `json:"destination"`
	Launcher    string `json:"launcher"`
	Method      string `json:"method"`
	Code        string `json:"code"`
	Model       string `json:"model"`
}
type RuntimeFlowAction struct {
	ID       string           `json:"id"`
	Revision string           `json:"revision"`
	Action   string           `json:"action"`
	Input    RuntimeFlowInput `json:"input"`
}
type RuntimeFlow struct {
	ID            string                 `json:"id"`
	Revision      string                 `json:"revision"`
	Sequence      int                    `json:"sequence"`
	Stage         string                 `json:"stage"`
	Title         string                 `json:"title"`
	Message       string                 `json:"message"`
	Installation  *RuntimeInstallation   `json:"installation"`
	Authorization *RuntimeAuthorization  `json:"authorization"`
	Launchers     []RuntimeConnectChoice `json:"launchers"`
	Methods       []RuntimeAuthMethod    `json:"methods"`
	Models        []RuntimeFlowModel     `json:"models"`
}
type RuntimeInstallation struct {
	Destination  string `json:"destination"`
	Source       string `json:"source"`
	Platform     string `json:"platform"`
	Instructions string `json:"instructions"`
	CanInstall   bool   `json:"canInstall"`
}
type RuntimeAuthorization struct {
	URL        string `json:"url"`
	UserCode   string `json:"userCode"`
	InputLabel string `json:"inputLabel"`
	ExpiresAt  string `json:"expiresAt"`
	CanSubmit  bool   `json:"canSubmit"`
}
type RuntimeAuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason"`
}
type RuntimeFlowModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RuntimeConfigurationController is available only to explicit settings actions.
// Neither application-scoped execution credentials nor model tools expose it.
type RuntimeConfigurationController interface {
	RuntimeConfiguration(context.Context) (RuntimeConfiguration, error)
	ChangeRuntimeConfiguration(context.Context, RuntimeConfigurationChange) (RuntimeMutationResult, error)
	RuntimeConnectionCatalog(context.Context, string, *RuntimeSettings) (RuntimeConnectionCatalog, error)
	StartRuntimeConnection(context.Context, RuntimeConnectionInput) (RuntimeFlow, error)
	AdvanceRuntimeConnection(context.Context, RuntimeFlowAction) (RuntimeFlow, error)
	WaitRuntimeConnection(context.Context, string, int) (RuntimeFlow, error)
	CancelRuntimeConnection(context.Context, string) error
}
