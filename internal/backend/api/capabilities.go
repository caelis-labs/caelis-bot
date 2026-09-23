package api

import "context"

// ProviderInfo is safe product metadata, never native configuration or authority.
type ProviderInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ConnectionKind string `json:"connectionKind"`
	HelpURL        string `json:"helpUrl"`
	ConnectionHint string `json:"connectionHint"`
}
type Provider interface{ ProviderInfo() ProviderInfo }

type Authenticator interface {
	Login(context.Context) (string, error)
	CancelLogin(context.Context) error
}
type ArtifactResolver interface{ Artifact(string) (string, error) }
type ApprovalNavigator interface{ ApprovalURL(string) (string, error) }

// These host-only ports describe implemented operations. Snapshot action flags
// still determine availability now; an interface never grants execution rights.
type SnapshotObserver interface {
	// Block until revision advances or context ends. Reconnection establishes a
	// new authoritative snapshot; old instance events must not revive decisions.
	WaitSnapshot(context.Context, uint64) (Snapshot, error)
}
type RecentSource interface{ RecentSnapshot() Snapshot }
type ComposerSource interface{ ComposerSnapshot() Snapshot }
type RevisionSource interface{ Revision() uint64 }
type HistorySource interface{ LoadEarlier(context.Context) error }
type DiagnosticSource interface{ DiagnosticStatus() map[string]any }
type RuntimeConfigurator interface {
	// Serialize with native submission/approval, reject busy or uncertain work,
	// validate independently, then persist before replacing the connection.
	ChangeRuntime(context.Context, RuntimeSettings, func() error) (RuntimeCheck, error)
}

// ExecutionSettingsSource returns Control-owned settings instead of a stale local cache.
type ExecutionSettingsSource interface {
	CurrentExecutionSettings(context.Context) (ExecutionSettings, error)
}
type ExecutionProvider interface {
	ExecutionOptions() ExecutionOptions
	Models(context.Context) ([]ModelOption, error)
	// Validate against the provider's current catalog/policy and persist before
	// applying; never rewrite an active turn or silently select another model.
	ChangeExecution(context.Context, ExecutionSettings, func() error) error
}
type AttachmentProvider interface {
	AttachmentStorage() (AttachmentStorage, error)
	TrashOldAttachments(context.Context, func(string) error) (AttachmentStorage, error)
}

type ExecutionOptions struct {
	DefaultApprovalMode string         `json:"defaultApprovalMode"`
	ApprovalModes       []ApprovalMode `json:"approvalModes"`
}
type ApprovalMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Dangerous   bool   `json:"dangerous"`
}

// ToolConnection is issued only by the native product host. The adapter maps
// per-tool approval into its own policy; no global/server approval is implied.
// It deliberately has no JSON/UI contract and must never enter diagnostics.
type ToolConnection struct {
	// Role instructions and callbacks are application-owned, never hardcoded by
	// a protocol adapter. The native MCP transport uses Command/Args/Env; another
	// adapter can bind Host to a session-scoped client-tool transport.
	Instructions, WorkerInstructions string
	Host                             ApplicationTools
	Command                          string
	Args                             []string
	Env                              map[string]string
	ApprovedTools                    []string
}
type BotToolBinder interface{ ConfigureBotTools(*ToolConnection) error }

func (c *ToolConnection) Clone() *ToolConnection {
	if c == nil {
		return nil
	}
	v := *c
	v.Args = append([]string(nil), c.Args...)
	v.ApprovedTools = append([]string(nil), c.ApprovedTools...)
	v.Env = make(map[string]string, len(c.Env))
	for key, value := range c.Env {
		v.Env[key] = value
	}
	return &v
}
