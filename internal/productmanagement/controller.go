package productmanagement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type installer interface {
	Manage(context.Context, runtimemanagement.Request) (runtimemanagement.Status, error)
}

type Controller struct {
	scope         Scope
	installation  installer
	configuration Configuration
	releases      []ReviewedRelease
}

var _ Port = (*Controller)(nil)

// New binds a target-local private directory supplied by trusted native node
// assembly. Neither the directory nor the configuration client has wire fields.
func New(scope Scope, directory string, configuration Configuration) (*Controller, error) {
	if !validScope(scope) {
		return nil, errors.New("invalid native management scope")
	}
	manager, err := runtimemanagement.New(directory)
	if err != nil {
		return nil, err
	}
	var releases []ReviewedRelease
	for _, release := range runtimemanagement.Releases() {
		if release.Arch == runtime.GOARCH {
			releases = append(releases, ReviewedRelease{Runtime: release.Runtime, Version: release.Version})
		}
	}
	return &Controller{scope: scope, installation: manager, configuration: configuration, releases: releases}, nil
}

func (c *Controller) Capabilities() Capabilities {
	// The existing native configuration API returns its original OperationID,
	// but provides no operation lookup on this port. Never infer a receipt by
	// comparing current model values after an uncertain mutation.
	return Capabilities{Installation: c.installation != nil, Configuration: c.configuration != nil, ConfigurationReceiptLookup: false}
}

func (c *Controller) check(scope Scope) error {
	if !validScope(scope) || scope != c.scope {
		return errors.New("management scope changed; inspect the connection")
	}
	return nil
}

func (c *Controller) ReviewedReleases(scope Scope) ([]ReviewedRelease, error) {
	if err := c.check(scope); err != nil {
		return nil, err
	}
	return append([]ReviewedRelease(nil), c.releases...), nil
}

func (c *Controller) RuntimeStatus(ctx context.Context, scope Scope, provider string) (runtimemanagement.Status, error) {
	if err := c.check(scope); err != nil {
		return runtimemanagement.Status{}, err
	}
	if c.installation == nil || !validRuntime(provider) {
		return runtimemanagement.Status{}, errors.New("runtime management unavailable")
	}
	return c.installation.Manage(ctx, runtimemanagement.Request{Action: "detect", Runtime: provider})
}

// Native request IDs include the inspected scope, while product results retain
// the original command ID. Runtime installer receipts remain queryable without
// permitting a different Bot/generation to adopt another installation intent.
func scopedRequestID(scope Scope, id string) string {
	encoded, _ := json.Marshal(struct {
		Scope
		ID string
	}{scope, id})
	sum := sha256.Sum256(encoded)
	return "product-" + hex.EncodeToString(sum[:])
}

func (c *Controller) ManageRuntime(ctx context.Context, command RuntimeCommand) (RuntimeResult, error) {
	result := RuntimeResult{Scope: c.scope, ID: command.ID, Outcome: "rejected", Code: "invalid-command"}
	if c.check(command.Scope) != nil || !validRuntimeCommand(command) {
		return result, nil
	}
	if c.installation == nil {
		result.Code = "unavailable"
		return result, nil
	}
	if ctx.Err() != nil {
		result.Code = "cancelled-before-dispatch"
		return result, nil
	}
	nativeID := scopedRequestID(command.Scope, command.ID)
	status, err := c.installation.Manage(ctx, runtimemanagement.Request{Action: command.Action, Runtime: command.Runtime, Version: command.Version, ExpectedVersion: command.ExpectedVersion, RequestID: nativeID})
	if status.Runtime != command.Runtime || status.RequestID != nativeID || !validOutcome(status.Outcome) {
		result.Outcome, result.Code = "unknown", "invalid-native-receipt"
		return result, nil
	}
	if err != nil && status.Outcome == "accepted" {
		result.Outcome, result.Code = "unknown", "native-operation-unresolved"
		return result, nil
	}
	status.RequestID = command.ID
	result.Outcome, result.Code, result.Status = status.Outcome, "", status
	if result.Outcome == "unknown" {
		result.Code = "native-operation-unresolved"
	}
	return result, nil
}

func (c *Controller) RuntimeConfiguration(ctx context.Context, scope Scope) (api.RuntimeConfiguration, error) {
	if err := c.check(scope); err != nil {
		return api.RuntimeConfiguration{}, err
	}
	if c.configuration == nil {
		return api.RuntimeConfiguration{}, errors.New("runtime configuration unavailable")
	}
	return c.configuration.RuntimeConfiguration(ctx)
}

func (c *Controller) ChangeRuntimeConfiguration(ctx context.Context, command ConfigurationCommand) (ConfigurationResult, error) {
	result := ConfigurationResult{Scope: c.scope, ID: command.ID, Outcome: "rejected", Code: "invalid-command"}
	if c.check(command.Scope) != nil || !identifier.MatchString(command.ID) || !ValidConfigurationChange(command.Change) {
		return result, nil
	}
	if c.configuration == nil {
		result.Code = "unavailable"
		return result, nil
	}
	if ctx.Err() != nil {
		result.Code = "cancelled-before-dispatch"
		return result, nil
	}
	// Exactly one existing native call. The enclosing durable product journal
	// owns replay and unknown fencing; this port never retries or reads values
	// to guess whether the native operation happened.
	native, err := c.configuration.ChangeRuntimeConfiguration(ctx, command.Change)
	result.Native = native
	if err != nil {
		result.Outcome, result.Code = "unknown", "native-operation-unresolved"
		return result, nil
	}
	if !identifier.MatchString(native.OperationID) {
		result.Outcome, result.Code = "unknown", "invalid-native-receipt"
		return result, nil
	}
	// Configuration uses Caelis command outcomes, while installation uses
	// product outcomes. Preserve the native receipt and translate only the
	// outer product receipt; a committed configuration is an accepted command.
	switch native.Outcome {
	case "committed":
		result.Outcome, result.Code = "accepted", ""
	case "conflicted":
		result.Outcome, result.Code = "rejected", "configuration-conflict"
	case "rejected":
		result.Outcome, result.Code = "rejected", ""
	case "unknown":
		result.Outcome, result.Code = "unknown", "native-operation-unresolved"
	default:
		result.Outcome, result.Code = "unknown", "invalid-native-receipt"
	}
	return result, nil
}
