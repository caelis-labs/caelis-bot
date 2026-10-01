package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type nativeConfigurationFixture struct {
	change api.RuntimeConfigurationChange
	calls  int
}

func (c *nativeConfigurationFixture) RuntimeConfiguration(context.Context) (api.RuntimeConfiguration, error) {
	return api.RuntimeConfiguration{Revision: "42", Main: api.WorkExecutionSettings{Model: "public/model"}}, nil
}
func (c *nativeConfigurationFixture) ChangeRuntimeConfiguration(_ context.Context, change api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	c.calls++
	c.change = change
	return api.RuntimeMutationResult{OperationID: "target-native-operation", Outcome: "committed", Message: "Configuration saved"}, nil
}

type managementProductPort struct{ *thinProductPort }

type configurationPortFixture struct {
	scope         productmanagement.Scope
	configuration *nativeConfigurationFixture
}

func (*configurationPortFixture) Capabilities() productmanagement.Capabilities {
	return productmanagement.Capabilities{Configuration: true}
}
func (*configurationPortFixture) ReviewedReleases(productmanagement.Scope) ([]productmanagement.ReviewedRelease, error) {
	return nil, nil
}
func (*configurationPortFixture) RuntimeStatus(context.Context, productmanagement.Scope, string) (runtimemanagement.Status, error) {
	return runtimemanagement.Status{}, productrpc.ErrUnsupported
}
func (*configurationPortFixture) ManageRuntime(context.Context, productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
	return productmanagement.RuntimeResult{}, productrpc.ErrUnsupported
}
func (c *configurationPortFixture) RuntimeConfiguration(ctx context.Context, scope productmanagement.Scope) (api.RuntimeConfiguration, error) {
	if scope != c.scope {
		return api.RuntimeConfiguration{}, productrpc.ErrUnsupported
	}
	return c.configuration.RuntimeConfiguration(ctx)
}
func (c *configurationPortFixture) ChangeRuntimeConfiguration(ctx context.Context, command productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error) {
	native, err := c.configuration.ChangeRuntimeConfiguration(ctx, command.Change)
	return productmanagement.ConfigurationResult{Scope: c.scope, ID: command.ID, Outcome: "accepted", Native: native}, err
}

func (*managementProductPort) TaskSummaries() []api.TaskSummary {
	return []api.TaskSummary{{ID: "target-native-task", Title: "Fixture work", Status: "interrupted", Outcome: "unknown", Pinned: true}}
}

func TestRemoteManagementActualFramedClientAndNativeReceiptObserver(t *testing.T) {
	port := &managementProductPort{&thinProductPort{snapshot: api.Snapshot{Connection: "ready", CanSend: true}}}
	configuration := &nativeConfigurationFixture{}
	token := strings.Repeat("synthetic-product-only-", 3)
	server, err := productrpc.NewServer(port, productrpc.Options{NodeID: "node-fixture", BotID: "bot-fixture", Token: token, JournalFile: filepath.Join(t.TempDir(), "journal.json"), Management: func(scope productmanagement.Scope) (productmanagement.Port, error) {
		return &configurationPortFixture{scope: scope, configuration: configuration}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	port.server = server
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		local, remote := net.Pipe()
		t.Cleanup(func() { _ = remote.Close() })
		go func() { _ = productrpc.ProxyStdio(t.Context(), remote, remote, host.URL, token) }()
		client, err := productrpc.NewStdioClient(productrpc.StdioOptions{ExpectedNode: "node-fixture", ExpectedBot: "bot-fixture"}, local)
		return client, local, err
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := make(chan []api.TaskSummary, 1)
	a := &Application{root: e.root, product: e, engine: e, Backend: backend.NewService(e, nil, nil, nil, nil), host: Host{ObserveTaskReceipts: func(summaries []api.TaskSummary) {
		if len(summaries) > 0 {
			select {
			case observed <- summaries:
			default:
			}
		}
	}}}
	if err = a.startRemoteProduct(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	select {
	case summaries := <-observed:
		if len(summaries) != 1 || summaries[0].ID == "target-native-task" || summaries[0].Status != "interrupted" || summaries[0].Outcome != "unknown" {
			t.Fatal("framed native facts changed", summaries)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remote task receipts did not reach native APP callback")
	}
	state, err := e.RemoteRuntime(t.Context())
	if err != nil || !state.Available || !state.Capabilities.Configuration || state.Capabilities.ConfigurationReceiptLookup {
		t.Fatal(state, err)
	}
	result, err := e.ChangeRemoteRuntimeConfiguration(t.Context(), backend.RemoteConfigurationRequest{ID: "framed-settings", Binding: state.Binding, Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/model", Effort: "medium"}}})
	if err != nil || result.Outcome != "accepted" || result.Configuration == nil || result.Configuration.Outcome != "committed" || result.Configuration.OperationID != "" || configuration.calls != 1 || configuration.change.ExpectedRevision != "42" {
		t.Fatalf("actual typed settings receipt: %+v %v", result, err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.stops != 0 {
		t.Fatal("APP detach stopped target Bot")
	}
}

type managementFixture struct {
	*thinClientFixture
	changes           int
	installs          int
	lastConfiguration productmanagement.ConfigurationCommand
	lastRuntime       productmanagement.RuntimeCommand
	configuration     api.RuntimeConfiguration
	change            func(productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error)
	install           func(productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error)
}

func (c *managementFixture) Connect(ctx context.Context) (productrpc.Identity, error) {
	identity, err := c.thinClientFixture.Connect(ctx)
	identity.Capabilities.RuntimeManagement = true
	return identity, err
}
func (c *managementFixture) ManagementCapabilities(context.Context) (productmanagement.Capabilities, error) {
	return productmanagement.Capabilities{Installation: true, Configuration: true}, nil
}
func (c *managementFixture) ReviewedReleases(context.Context) ([]productmanagement.ReviewedRelease, error) {
	return []productmanagement.ReviewedRelease{{Runtime: "caelis", Version: "0.65.0"}}, nil
}
func (c *managementFixture) RuntimeStatus(_ context.Context, runtime string) (runtimemanagement.Status, error) {
	return runtimemanagement.Status{Runtime: runtime, Installed: true, Version: "0.65.0"}, nil
}
func (c *managementFixture) RuntimeConfiguration(context.Context) (api.RuntimeConfiguration, error) {
	return c.configuration, nil
}
func (c *managementFixture) ChangeRuntimeConfiguration(_ context.Context, command productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error) {
	c.changes++
	c.lastConfiguration = command
	if c.change != nil {
		return c.change(command)
	}
	return productmanagement.ConfigurationResult{Scope: command.Scope, ID: command.ID, Outcome: "accepted", Native: api.RuntimeMutationResult{OperationID: "private-operation", Outcome: "committed", Message: "Configuration saved"}}, nil
}
func (c *managementFixture) ManageRuntime(_ context.Context, command productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
	c.installs++
	c.lastRuntime = command
	if c.install != nil {
		return c.install(command)
	}
	return productmanagement.RuntimeResult{Scope: command.Scope, ID: command.ID, Outcome: "accepted", Status: runtimemanagement.Status{Runtime: command.Runtime, RequestID: command.ID, Outcome: "accepted", Installed: true, Version: command.Version}}, nil
}
func managedEngine(t *testing.T) (*productEngine, *managementFixture, backend.RemoteRuntimeState) {
	t.Helper()
	fixture := &managementFixture{thinClientFixture: newThinClientFixture(), configuration: api.RuntimeConfiguration{Revision: "42", Main: api.WorkExecutionSettings{Model: "public/model"}}}
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		return fixture, &thinCloserFixture{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := e.RemoteRuntime(t.Context())
	if err != nil || !state.Available || state.Binding == "" {
		t.Fatalf("inspection: %+v %v", state, err)
	}
	return e, fixture, state
}
func TestRemoteManagementUsesNativeScopeAndRejectsDetachedSettings(t *testing.T) {
	e, c, state := managedEngine(t)
	request := backend.RemoteConfigurationRequest{ID: "settings-1", Binding: state.Binding, Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/model"}}}
	result, err := e.ChangeRemoteRuntimeConfiguration(t.Context(), request)
	if err != nil || result.Outcome != "accepted" || result.Configuration.Outcome != "committed" || result.Configuration.OperationID != "" || c.lastConfiguration.Scope.BotID != "bot-fixture" || c.lastConfiguration.Scope.Generation != "generation-one" {
		t.Fatalf("native scope/projection: %+v %v %+v", result, err, c.lastConfiguration)
	}
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	request.ID = "new-edit"
	if _, err = e.ChangeRemoteRuntimeConfiguration(t.Context(), request); err == nil || c.changes != 1 {
		t.Fatal("stale connection binding dispatched settings")
	}
	if _, err = e.RemoteRuntimeConfiguration(t.Context(), state.Binding); err == nil {
		t.Fatal("stale connection read settings")
	}
	if err = e.detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ManageRemoteRuntime(t.Context(), backend.RemoteRuntimeRequest{ID: "install-1", Binding: state.Binding, Action: "install", Runtime: "caelis", Version: "0.65.0"}); err == nil || c.installs != 0 {
		t.Fatal("detached target dispatched installation")
	}
}
func TestRemoteConfigurationUnknownSurvivesMatchingReadAndBlocksReplacement(t *testing.T) {
	e, c, state := managedEngine(t)
	c.change = func(command productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error) {
		c.configuration.Main = command.Change.Selection
		return productmanagement.ConfigurationResult{Scope: command.Scope, ID: command.ID, Outcome: "unknown", Native: api.RuntimeMutationResult{OperationID: "native-secret-id", Outcome: "unknown"}}, errors.New("response lost")
	}
	request := backend.RemoteConfigurationRequest{ID: "original-edit", Binding: state.Binding, Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/new-model"}}}
	result, err := e.ChangeRemoteRuntimeConfiguration(t.Context(), request)
	if err == nil || result.Outcome != "unknown" || e.Snapshot().CanSend {
		t.Fatal("unknown did not fence admission")
	}
	c.result = productrpc.Result{Outcome: "unknown"}
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = e.RemoteRuntime(t.Context())
	if err != nil || len(state.Pending) != 1 || state.Capabilities.ConfigurationReceiptLookup {
		t.Fatalf("unknown observation: %+v %v", state, err)
	}
	configuration, err := e.RemoteRuntimeConfiguration(t.Context(), state.Binding)
	if err != nil || configuration.Main.Model != "public/new-model" {
		t.Fatal(configuration, err)
	}
	if e.Snapshot().Phase != "unknown" || e.Snapshot().CanSend {
		t.Fatal("matching values inferred committed original receipt")
	}
	request.ID, request.Binding = "replacement-edit", state.Binding
	if _, err = e.ChangeRemoteRuntimeConfiguration(t.Context(), request); err == nil || c.changes != 1 {
		t.Fatal("fresh edit replaced unknown original")
	}
	c.result = productrpc.Result{Outcome: "accepted"}
	result, err = e.ReconcileRemoteManagement(t.Context(), state.Binding, "original-edit")
	if err != nil || result.Outcome != "accepted" || c.changes != 1 || len(e.receipts.Pending) != 0 {
		t.Fatal("receipt reconciliation replayed edit", result, err)
	}
	b, err := os.ReadFile(filepath.Join(e.root, "product-client-receipts.json"))
	if err != nil || strings.Contains(string(b), "public/new-model") || strings.Contains(string(b), "native-secret-id") {
		t.Fatal("private receipt metadata included model intent or native binding", err)
	}
}
func TestRemoteInstallationResolveKeepsExactOriginalIntent(t *testing.T) {
	e, c, state := managedEngine(t)
	c.install = func(command productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
		return productmanagement.RuntimeResult{Scope: command.Scope, ID: command.ID, Outcome: "unknown"}, nil
	}
	request := backend.RemoteRuntimeRequest{ID: "original-install", Binding: state.Binding, Action: "install", Runtime: "caelis", Version: "0.65.0"}
	if result, err := e.ManageRemoteRuntime(t.Context(), request); err == nil || result.Outcome != "unknown" {
		t.Fatal(result, err)
	}
	c.result = productrpc.Result{Outcome: "unknown"}
	if err := e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := e.RemoteRuntime(t.Context())
	if err != nil || len(state.Pending) != 1 {
		t.Fatal(state, err)
	}
	request.Binding, request.Action, request.Version = state.Binding, "resolve", "other-version"
	if _, err = e.ManageRemoteRuntime(t.Context(), request); err == nil || c.installs != 1 {
		t.Fatal("changed intent resolved a different operation")
	}
	c.install = nil
	request.Version = "0.65.0"
	result, err := e.ManageRemoteRuntime(t.Context(), request)
	if err != nil || result.Outcome != "accepted" || c.installs != 2 || c.lastRuntime.ID != "original-install" || c.lastRuntime.Action != "resolve" {
		t.Fatal("original install recovery lost its identity", result, err, c.lastRuntime)
	}
}

func TestRemoteInstallationReceiptSurvivesClientAndTargetRestart(t *testing.T) {
	e, c, state := managedEngine(t)
	originalScope := productmanagement.Scope{BotID: "bot-fixture", Generation: "generation-one"}
	c.install = func(command productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
		return productmanagement.RuntimeResult{Scope: command.Scope, ID: command.ID, Outcome: "unknown"}, nil
	}
	request := backend.RemoteRuntimeRequest{ID: "original-install", Binding: state.Binding, Action: "install", Runtime: "caelis", Version: "0.65.0"}
	if result, err := e.ManageRemoteRuntime(t.Context(), request); err == nil || result.Outcome != "unknown" {
		t.Fatal(result, err)
	}
	if err := e.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.state.Scope.Generation, c.state.Cursor.Generation = "generation-two", "generation-two"
	c.result = productrpc.Result{Outcome: "unknown"}
	c.mu.Unlock()
	restarted, err := newProductEngine(e.root, thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		return c, &thinCloserFixture{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close(context.Background()) })
	if err = restarted.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = restarted.RemoteRuntime(t.Context())
	if err != nil || len(state.Pending) != 1 {
		t.Fatal("original intent was lost across client restart", state, err)
	}
	request.Binding, request.Action = state.Binding, "resolve"
	c.install = func(command productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
		return productmanagement.RuntimeResult{Scope: originalScope, ID: command.ID, Outcome: "unknown", Code: "original-scope-unavailable"}, nil
	}
	if result, err := restarted.ManageRemoteRuntime(t.Context(), request); err == nil || result.Outcome != "unknown" || restarted.Snapshot().CanSend {
		t.Fatal("owner restart inferred a known original receipt", result, err)
	}
	if err = restarted.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = restarted.RemoteRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request.Binding = state.Binding
	c.install = func(command productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
		return productmanagement.RuntimeResult{Scope: originalScope, ID: command.ID, Outcome: "accepted", Status: runtimemanagement.Status{Runtime: command.Runtime, RequestID: command.ID, Outcome: "accepted", Installed: true, Version: command.Version}}, nil
	}
	result, err := restarted.ManageRemoteRuntime(t.Context(), request)
	if err != nil || result.Outcome != "accepted" || len(restarted.receipts.Pending) != 0 || c.lastRuntime.ID != "original-install" || c.lastRuntime.Action != "resolve" || c.lastRuntime.Scope.Generation != "generation-two" {
		t.Fatal("known original receipt was rebound or replayed after owner restart", result, err)
	}
}
