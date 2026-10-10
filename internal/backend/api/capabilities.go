package api

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// ErrRecoveryPending means the original ingress was refused before native
// dispatch. Its request ID may be retried after the same owner's recovery settles.
var ErrRecoveryPending = errors.New("runtime_recovery_pending")

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

// NativePluginSource exposes installed, enabled plugins from the selected
// Runtime's own catalog. It is a read-only menu directory, not a permission or
// per-turn plugin selection.
type NativePluginSource interface {
	NativePlugins(context.Context) ([]NativePlugin, error)
}

type NativePlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// RecoveryState is a host-only, opaque fence for one Runtime connection owner.
// A channel may offer manual recovery only after automatic recovery stops.
type RecoveryState struct {
	Fence      string
	Automatic  bool
	InProgress bool
	Manual     bool
}
type RecoverySource interface{ RecoveryState() RecoveryState }
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
	// applying at its documented request boundary; never rewrite issued requests.
	ChangeExecution(context.Context, ExecutionSettings, func() error) error
}
type WorkExecutionProvider interface {
	// Persist before applying to new work; never changes an existing task.
	ChangeWorkExecution(context.Context, WorkExecutionSettings, func() error) error
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
	// SkillInstructions is the resident metadata guide for adapters that do not
	// publish selected Skill metadata themselves. Skill bodies stay on disk.
	SkillInstructions string
	// Notebook is the resident Bot's work directory, never a worker workspace.
	NotebookDirectory string
	// RuntimeVersion identifies the product assembly used for new resident contexts.
	RuntimeVersion string
	// PrepareTurn refreshes host-owned local metadata before submission; FinishTurn
	// runs after authoritative resident completion, outside adapter locks.
	PrepareTurn    func(context.Context) error
	FinishTurn     func()
	PrepareContext func(context.Context) (ContextSeed, error)
	ConsumeContext func(ContextSeed) error
	Host           ApplicationTools
	Command        string
	Args           []string
	Env            map[string]string
	ApprovedTools  []string
	// Services partitions the compact built-in catalog for new Codex resident
	// threads. Names are protocol identities, never user-facing plugin titles.
	Services          []ToolService
	Plugins           plugins.Selection
	BuiltinSkillRoots []string
}

// PluginConfigurator applies Bot-owned package selection in the Runtime.
// Worker execution never receives this port.
type PluginConfigurator interface {
	UpdateBotPlugins(context.Context, plugins.Selection) error
	// Serialize one automatic projection with the Runtime's current turn.
	// The host can commit package state independently while this waits.
	WithBotPluginAdmission(context.Context, func(func(context.Context, plugins.Selection) error) error) error
	BotPluginHealth(context.Context) []plugins.Issue
}

// PluginInspector reads only a Bot-selected server's public directory. It must
// never call a tool or start discovery for an unselected server.
type PluginInspector interface {
	BotPluginServer(context.Context, string) (plugins.ServerDetail, error)
	BotPluginGeneration() uint64
}
type ToolService struct {
	Name  string
	Tools []string
}
type BotToolBinder interface{ ConfigureBotTools(*ToolConnection) error }

func (c *ToolConnection) Clone() *ToolConnection {
	if c == nil {
		return nil
	}
	v := *c
	v.Args = append([]string(nil), c.Args...)
	v.ApprovedTools = append([]string(nil), c.ApprovedTools...)
	v.Services = make([]ToolService, len(c.Services))
	for i, service := range c.Services {
		v.Services[i] = ToolService{Name: service.Name, Tools: append([]string(nil), service.Tools...)}
	}
	v.Plugins = c.Plugins.Clone()
	v.BuiltinSkillRoots = append([]string(nil), c.BuiltinSkillRoots...)
	v.Env = make(map[string]string, len(c.Env))
	for key, value := range c.Env {
		v.Env[key] = value
	}
	return &v
}
