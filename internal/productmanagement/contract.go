// Package productmanagement is an optional typed native settings port. It owns
// no transport, SSH target, credentials, model tools, or resident Bot lifecycle.
package productmanagement

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

// Scope is the exact identity inspected through the product connection. It is
// deliberately independent of productrpc so that its optional port can use it.
type Scope struct {
	BotID      string `json:"botId"`
	Generation string `json:"generation"`
}

type Capabilities struct {
	Installation               bool `json:"installation"`
	Configuration              bool `json:"configuration"`
	ConfigurationReceiptLookup bool `json:"configurationReceiptLookup"`
}

// Configuration is only the nonsecret subset of the existing settings port.
// Credential setup, custom URLs, commands and authorization flows stay native.
type Configuration interface {
	RuntimeConfiguration(context.Context) (api.RuntimeConfiguration, error)
	ChangeRuntimeConfiguration(context.Context, api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error)
}

type ReviewedRelease struct {
	Runtime string `json:"runtime"`
	Version string `json:"version"`
}

type RuntimeCommand struct {
	Scope
	ID              string `json:"id"`
	Action          string `json:"action"`
	Runtime         string `json:"runtime"`
	Version         string `json:"version,omitempty"`
	ExpectedVersion string `json:"expectedVersion,omitempty"`
}

type RuntimeResult struct {
	Scope
	ID      string                   `json:"id"`
	Outcome string                   `json:"outcome"`
	Code    string                   `json:"code,omitempty"`
	Status  runtimemanagement.Status `json:"status"`
}

type ConfigurationCommand struct {
	Scope
	ID     string                         `json:"id"`
	Change api.RuntimeConfigurationChange `json:"change"`
}

type ConfigurationResult struct {
	Scope
	ID      string                    `json:"id"`
	Outcome string                    `json:"outcome"`
	Code    string                    `json:"code,omitempty"`
	Native  api.RuntimeMutationResult `json:"native"`
}

// Port is bound once to native identity and target-local installation authority.
// Before any mutation, the owning product service must durably journal its full
// scoped command ID and digest, dispatch once, and reconcile that same receipt.
// An observation cancellation must not cancel an admitted native operation.
type Port interface {
	Capabilities() Capabilities
	ReviewedReleases(Scope) ([]ReviewedRelease, error)
	RuntimeStatus(context.Context, Scope, string) (runtimemanagement.Status, error)
	ManageRuntime(context.Context, RuntimeCommand) (RuntimeResult, error)
	RuntimeConfiguration(context.Context, Scope) (api.RuntimeConfiguration, error)
	ChangeRuntimeConfiguration(context.Context, ConfigurationCommand) (ConfigurationResult, error)
}
