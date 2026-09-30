// Package productrpc is the optional native connection to one resident Bot.
// It does not select a Runtime, create a local Bot, or own the SSH tunnel.
package productrpc

import (
	"context"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

const (
	ProtocolVersion  = 1
	MaxCommandBytes  = 512 << 10
	MaxSnapshotBytes = 8 << 20
	MaxResourceBytes = 20 << 20
	MaxReceipts      = 4096
	MaxTaskSummaries = 512
)

var ErrUnsupported = errors.New("product capability unavailable")

// Scope binds commands to the service the native client actually inspected.
// BotID is an opaque product identity; Generation changes at each service start.
type Scope struct {
	BotID      string `json:"botId"`
	Generation string `json:"generation"`
}

type Capabilities struct {
	Files             bool `json:"files"`
	Interrupt         bool `json:"interrupt"`
	RuntimeManagement bool `json:"runtimeManagement"`
	Execution         bool `json:"execution"`
}

type Identity struct {
	Version int    `json:"version"`
	NodeID  string `json:"nodeId"`
	Scope
	Capabilities Capabilities `json:"capabilities"`
}

// Cursor uses a decimal string so a native revision never loses precision in JS.
type Cursor struct {
	Generation string `json:"generation"`
	Revision   string `json:"revision"`
}

type State struct {
	Scope
	Cursor          Cursor                `json:"cursor"`
	Reset           bool                  `json:"reset"`
	Snapshot        api.Snapshot          `json:"snapshot"`
	Initialization  api.BotInitialization `json:"initialization"`
	Draft           api.Draft             `json:"draft"`
	ApprovalTargets map[string]string     `json:"approvalTargets"`
	TaskSummaries   []api.TaskSummary     `json:"taskSummaries"`
}

// Command is a closed union. Exactly the payload for Kind is allowed.
// Native paths, ToolConnection and provider credentials have no wire fields.
type Command struct {
	Scope
	ID                string                                  `json:"id"`
	Kind              string                                  `json:"kind"`
	Submission        *api.Submission                         `json:"submission,omitempty"`
	Decision          *ApprovalDecision                       `json:"decision,omitempty"`
	Turn              string                                  `json:"turn,omitempty"`
	Introduction      *api.BotIntroduction                    `json:"introduction,omitempty"`
	Draft             *api.Draft                              `json:"draft,omitempty"`
	RuntimeManagement *productmanagement.RuntimeCommand       `json:"runtimeManagement,omitempty"`
	Configuration     *productmanagement.ConfigurationCommand `json:"configuration,omitempty"`
	Execution         *productmanagement.ExecutionCommand     `json:"execution,omitempty"`
}

type ApprovalDecision struct {
	Target string `json:"target"`
	api.Decision
}

// Result is a command receipt, not evidence that a model turn completed.
type Result struct {
	ID                string                                 `json:"id"`
	Outcome           string                                 `json:"outcome"`
	Code              string                                 `json:"code,omitempty"`
	Submission        *api.Receipt                           `json:"submission,omitempty"`
	Initialization    *api.BotInitialization                 `json:"initialization,omitempty"`
	Draft             *api.Draft                             `json:"draft,omitempty"`
	RuntimeManagement *productmanagement.RuntimeResult       `json:"runtimeManagement,omitempty"`
	Configuration     *productmanagement.ConfigurationResult `json:"configuration,omitempty"`
	Execution         *productmanagement.ExecutionResult     `json:"execution,omitempty"`
}

// Port is backed by the composed product Service, including Worker facts.
// Request cancellation ends observation; it must never implicitly stop the Bot.
// Interrupt must validate the exact observed turn at its native dispatch boundary.
type Port interface {
	Snapshot() api.Snapshot
	BotInitialization() api.BotInitialization
	Draft() api.Draft
	SaveDraft(api.Draft) (api.Draft, error)
	Submit(context.Context, api.Submission) (api.Receipt, error)
	InitializeBot(context.Context, api.BotIntroduction) (api.BotInitialization, error)
	RetryBotIntroduction(context.Context) (api.BotInitialization, error)
	Decide(context.Context, api.Decision) error
	InterruptTurn(context.Context, string) error
	LoadEarlier(context.Context) error
	StopBot(context.Context) error
}

type Resource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Resources validates Bot/task ownership before returning bytes. Native paths
// remain behind this port, including task-owned api.WorkArtifactProvider paths.
type Resources interface {
	Open(context.Context, string) (Resource, io.ReadCloser, error)
	Upload(context.Context, Resource, io.Reader) (Resource, error)
}

// TaskSummaryPort is a host-only composed watchlist observation. It carries
// native terminal status and receipt uncertainty independently, never paths.
type TaskSummaryPort interface{ TaskSummaries() []api.TaskSummary }
