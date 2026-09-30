package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

// This optional native port consumes only the reviewed product settings wire.
// Credentials, authentication flows, paths and arbitrary endpoints stay absent.
type nativeProductManagement interface {
	ManagementCapabilities(context.Context) (productmanagement.Capabilities, error)
	ReviewedReleases(context.Context) ([]productmanagement.ReviewedRelease, error)
	RuntimeStatus(context.Context, string) (runtimemanagement.Status, error)
	ManageRuntime(context.Context, productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error)
	RuntimeConfiguration(context.Context) (api.RuntimeConfiguration, error)
	ChangeRuntimeConfiguration(context.Context, productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error)
}

func (e *productEngine) managementBindingLocked() string {
	b, _ := json.Marshal(struct {
		Scope   any
		Attempt uint64
	}{e.identity.Scope, e.attempt})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (e *productEngine) managementClient(binding string) (nativeProductClient, nativeProductManagement, productmanagement.Scope, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.client == nil || e.connection != "ready" || !e.identity.Capabilities.RuntimeManagement || binding != "" && binding != e.managementBindingLocked() {
		return nil, nil, productmanagement.Scope{}, errors.New("remote management connection changed; inspect its current state")
	}
	managed, ok := e.client.(nativeProductManagement)
	if !ok {
		return nil, nil, productmanagement.Scope{}, errors.New("remote Runtime management is unavailable")
	}
	return e.client, managed, productmanagement.Scope{BotID: e.identity.BotID, Generation: e.identity.Generation}, nil
}

func (e *productEngine) managementCurrent(binding string, client nativeProductClient) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.closed && e.client == client && e.connection == "ready" && binding == e.managementBindingLocked()
}

func (e *productEngine) managementContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	stop := context.AfterFunc(e.life, cancel)
	return ctx, func() { stop(); cancel() }
}

func (e *productEngine) RemoteRuntime(parent context.Context) (backend.RemoteRuntimeState, error) {
	e.op.Lock()
	defer e.op.Unlock()
	state := backend.RemoteRuntimeState{Label: e.pairing.Label, Releases: []productmanagement.ReviewedRelease{}, Pending: []backend.RemoteRuntimePending{}}
	for id, pending := range e.receipts.Pending {
		if pending.Kind != "manage-runtime" && pending.Kind != "configure-runtime" {
			continue
		}
		row := backend.RemoteRuntimePending{ID: id, Kind: pending.Kind}
		if pending.Runtime != nil {
			runtime := *pending.Runtime
			row.Runtime = &runtime
		}
		state.Pending = append(state.Pending, row)
	}
	client, managed, _, err := e.managementClient("")
	if err != nil {
		return state, nil
	}
	e.mu.Lock()
	state.Binding = e.managementBindingLocked()
	e.mu.Unlock()
	ctx, done := e.managementContext(parent)
	defer done()
	state.Capabilities, err = managed.ManagementCapabilities(ctx)
	if err != nil {
		return state, errors.New("remote management capabilities could not be confirmed")
	}
	if state.Capabilities.Installation {
		state.Releases, err = managed.ReviewedReleases(ctx)
		if err != nil {
			return state, errors.New("reviewed target releases are unavailable")
		}
	}
	if !e.managementCurrent(state.Binding, client) {
		return backend.RemoteRuntimeState{}, errors.New("remote management connection changed")
	}
	state.Available = true
	for _, row := range state.Pending {
		if row.Runtime != nil {
			row.Runtime.Binding = state.Binding
		}
	}
	return state, nil
}

func (e *productEngine) RemoteRuntimeStatus(parent context.Context, binding, runtime string) (runtimemanagement.Status, error) {
	client, managed, _, err := e.managementClient(binding)
	if err != nil || binding == "" {
		return runtimemanagement.Status{}, errors.New("inspect the target before reading Runtime status")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	status, err := managed.RuntimeStatus(ctx, runtime)
	if err != nil || status.Runtime != runtime || !e.managementCurrent(binding, client) {
		return runtimemanagement.Status{}, errors.New("target Runtime status is unavailable or changed")
	}
	return status, nil
}

func (e *productEngine) RemoteRuntimeConfiguration(parent context.Context, binding string) (api.RuntimeConfiguration, error) {
	client, managed, _, err := e.managementClient(binding)
	if err != nil || binding == "" {
		return api.RuntimeConfiguration{}, errors.New("inspect the target before reading Runtime settings")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	configuration, err := managed.RuntimeConfiguration(ctx)
	if err != nil || !e.managementCurrent(binding, client) {
		return api.RuntimeConfiguration{}, errors.New("target Runtime settings are unavailable or changed")
	}
	return configuration, nil
}

func (e *productEngine) finishManagement(id string, result backend.RemoteManagementResult, err error) (backend.RemoteManagementResult, error) {
	if err != nil || result.ID != id || result.Outcome != "accepted" && result.Outcome != "rejected" {
		e.mu.Lock()
		e.offlineLocked("outcome_unknown")
		e.mu.Unlock()
		return backend.RemoteManagementResult{ID: id, Outcome: "unknown", Code: "original-outcome-unresolved"}, errors.New("original target operation remains unknown; reconcile its receipt before continuing")
	}
	delete(e.receipts.Pending, id)
	if err = e.writeReceipts(); err != nil {
		return result, errors.New("target receipt is known but local receipt metadata could not be updated")
	}
	return result, nil
}

func (e *productEngine) reserveManagement(id, kind string, scope productmanagement.Scope, intent any, runtime *backend.RemoteRuntimeRequest) error {
	if !productIdentifier.MatchString(id) {
		return errors.New("stable product operation identity is required")
	}
	if len(e.receipts.Pending) > 0 {
		return errors.New("reconcile the original operation before issuing another target change")
	}
	b, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	pending := productPending{Kind: kind, Digest: hex.EncodeToString(sum[:]), Scope: &scope}
	if runtime != nil {
		copy := *runtime
		copy.Binding = ""
		pending.Runtime = &copy
	}
	e.receipts.Pending[id] = pending
	if err = e.writeReceipts(); err != nil {
		return errors.New("original target operation intent could not be preserved; nothing was dispatched")
	}
	return nil
}

func (e *productEngine) ManageRemoteRuntime(parent context.Context, request backend.RemoteRuntimeRequest) (backend.RemoteManagementResult, error) {
	e.op.Lock()
	defer e.op.Unlock()
	client, managed, scope, err := e.managementClient(request.Binding)
	if err != nil || request.Binding == "" {
		return backend.RemoteManagementResult{}, errors.New("target Runtime connection changed; inspect before applying changes")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	caps, err := managed.ManagementCapabilities(ctx)
	if err != nil || !caps.Installation {
		return backend.RemoteManagementResult{}, errors.New("target Runtime installation is unavailable")
	}
	command := productmanagement.RuntimeCommand{Scope: scope, ID: request.ID, Action: request.Action, Runtime: request.Runtime, Version: request.Version, ExpectedVersion: request.ExpectedVersion}
	expectedReceiptScope := scope
	if request.Action == "resolve" {
		pending, exists := e.receipts.Pending[request.ID]
		if !exists || pending.Kind != "manage-runtime" || pending.Runtime == nil || pending.Scope == nil || pending.Scope.BotID != scope.BotID || pending.Runtime.Runtime != request.Runtime || pending.Runtime.Version != request.Version || pending.Runtime.ExpectedVersion != request.ExpectedVersion {
			return backend.RemoteManagementResult{}, errors.New("original installation intent changed; no recovery was dispatched")
		}
		expectedReceiptScope = *pending.Scope
	} else {
		if request.Action != "install" && request.Action != "update" {
			return backend.RemoteManagementResult{}, errors.New("unsupported target installation action")
		}
		if err = e.reserveManagement(request.ID, "manage-runtime", scope, command, &request); err != nil {
			return backend.RemoteManagementResult{}, err
		}
	}
	if !e.managementCurrent(request.Binding, client) {
		return backend.RemoteManagementResult{ID: request.ID, Outcome: "unknown"}, errors.New("target connection ended before an operation receipt was observed")
	}
	result, err := managed.ManageRuntime(ctx, command)
	if result.Scope != expectedReceiptScope && result.Outcome != "unknown" {
		err = errors.New("target installation receipt identity changed")
	}
	if result.Outcome == "accepted" && (result.Status.Runtime != request.Runtime || result.Status.RequestID != request.ID || result.Status.Outcome != "accepted") {
		err = errors.New("target installation receipt does not confirm the original operation")
	}
	status := result.Status
	return e.finishManagement(request.ID, backend.RemoteManagementResult{ID: result.ID, Outcome: result.Outcome, Code: result.Code, Status: &status}, err)
}

func (e *productEngine) ChangeRemoteRuntimeConfiguration(parent context.Context, request backend.RemoteConfigurationRequest) (backend.RemoteManagementResult, error) {
	e.op.Lock()
	defer e.op.Unlock()
	client, managed, scope, err := e.managementClient(request.Binding)
	if err != nil || request.Binding == "" {
		return backend.RemoteManagementResult{}, errors.New("target settings connection changed; inspect before applying changes")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	caps, err := managed.ManagementCapabilities(ctx)
	if err != nil || !caps.Configuration {
		return backend.RemoteManagementResult{}, errors.New("target Runtime settings are unavailable")
	}
	command := productmanagement.ConfigurationCommand{Scope: scope, ID: request.ID, Change: request.Change}
	if err = e.reserveManagement(request.ID, "configure-runtime", scope, command, nil); err != nil {
		return backend.RemoteManagementResult{}, err
	}
	if !e.managementCurrent(request.Binding, client) {
		return backend.RemoteManagementResult{ID: request.ID, Outcome: "unknown"}, errors.New("target connection ended before a settings receipt was observed")
	}
	result, err := managed.ChangeRuntimeConfiguration(ctx, command)
	if result.Scope != scope {
		err = errors.New("target settings receipt identity changed")
	}
	if result.Outcome == "accepted" && result.Native.Outcome != "committed" || result.Outcome == "rejected" && result.Native.Outcome != "" && result.Native.Outcome != "conflicted" && result.Native.Outcome != "rejected" {
		err = errors.New("target settings receipt outcome is inconsistent")
	}
	native := result.Native
	native.OperationID = "" // Native recovery identity stays target-side.
	return e.finishManagement(request.ID, backend.RemoteManagementResult{ID: result.ID, Outcome: result.Outcome, Code: result.Code, Configuration: &native}, err)
}

func (e *productEngine) ReconcileRemoteManagement(parent context.Context, binding, id string) (backend.RemoteManagementResult, error) {
	e.op.Lock()
	defer e.op.Unlock()
	client, _, _, err := e.managementClient(binding)
	if err != nil || binding == "" {
		return backend.RemoteManagementResult{}, errors.New("reconnect and inspect the original target before checking its receipt")
	}
	pending, exists := e.receipts.Pending[id]
	if !exists || pending.Kind != "manage-runtime" && pending.Kind != "configure-runtime" {
		return backend.RemoteManagementResult{}, errors.New("original target management receipt is unavailable")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	result, err := client.Receipt(ctx, id)
	if !e.managementCurrent(binding, client) {
		err = errors.New("target receipt connection changed")
	}
	return e.finishManagement(id, backend.RemoteManagementResult{ID: result.ID, Outcome: result.Outcome, Code: result.Code}, err)
}

var _ backend.RemoteManagementController = (*productEngine)(nil)
