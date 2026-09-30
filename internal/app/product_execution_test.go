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

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

type executionFixture struct {
	*managementFixture
	view    productmanagement.ExecutionView
	calls   int
	command productmanagement.ExecutionCommand
	change  func(productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error)
}

func (c *executionFixture) Connect(ctx context.Context) (productrpc.Identity, error) {
	identity, err := c.thinClientFixture.Connect(ctx)
	identity.Capabilities.Execution = true
	return identity, err
}
func (c *executionFixture) ManagementCapabilities(context.Context) (productmanagement.Capabilities, error) {
	return productmanagement.Capabilities{Execution: true}, nil
}
func (c *executionFixture) ExecutionSettings(context.Context) (productmanagement.ExecutionView, error) {
	return c.view, nil
}
func (c *executionFixture) ChangeExecutionSettings(_ context.Context, command productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error) {
	c.calls++
	c.command = command
	if c.change != nil {
		return c.change(command)
	}
	return productmanagement.ExecutionResult{Scope: command.Scope, ID: command.ID, Outcome: "accepted"}, nil
}
func executionEngine(t *testing.T) (*productEngine, *executionFixture, backend.RemoteRuntimeState) {
	t.Helper()
	c := &executionFixture{managementFixture: &managementFixture{thinClientFixture: newThinClientFixture()}, view: productmanagement.ExecutionView{Scope: productmanagement.Scope{BotID: "bot-fixture", Generation: "generation-one"}, Revision: strings.Repeat("a", 64), Conversation: productmanagement.Selection{Model: "public/model", Effort: "medium"}, Models: []api.ModelOption{{Model: "public/model", Efforts: []string{"medium", "high"}}}}}
	e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
		return c, &thinCloserFixture{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := e.RemoteRuntime(t.Context())
	if err != nil || !state.Available || !state.Capabilities.Execution || state.Capabilities.Configuration || state.Capabilities.Installation {
		t.Fatal(state, err)
	}
	return e, c, state
}
func TestRemoteExecutionIndependentCapabilityNativeScopeAndStaleBinding(t *testing.T) {
	e, c, state := executionEngine(t)
	service := backend.NewService(e, nil, nil, nil, nil)
	service.ConfigureRemoteManagement(e)
	view, err := service.RemoteExecutionSettings(t.Context(), state.Binding)
	if err != nil || view.Work != nil || view.ConversationDefault || view.Binding != state.Binding {
		t.Fatal(view, err)
	}
	request := backend.RemoteExecutionRequest{ID: "original-model", Binding: state.Binding, Target: "conversation", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{Model: "public/model", Effort: "high"}}
	result, err := service.ChangeRemoteExecutionSettings(t.Context(), request)
	if err != nil || result.Outcome != "accepted" || c.calls != 1 || c.command.Scope != c.view.Scope {
		t.Fatal(result, err, c.command)
	}
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	request.ID = "fresh-model"
	if _, err = service.ChangeRemoteExecutionSettings(t.Context(), request); err == nil || c.calls != 1 {
		t.Fatal("stale binding dispatched a model edit")
	}
	if _, err = service.RemoteExecutionSettings(t.Context(), state.Binding); err == nil {
		t.Fatal("stale binding read model settings")
	}
	current, err := e.RemoteRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c.view.Scope.Generation = "foreign-generation"
	if _, err = e.RemoteExecutionSettings(t.Context(), current.Binding); err == nil {
		t.Fatal("foreign scope exported model catalog")
	}
}
func TestRemoteExecutionUnknownMatchingReadAndOriginalReceiptOnly(t *testing.T) {
	e, c, state := executionEngine(t)
	c.change = func(command productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error) {
		c.view.Conversation = command.Selection
		return productmanagement.ExecutionResult{Scope: command.Scope, ID: command.ID, Outcome: "unknown"}, errors.New("response lost")
	}
	request := backend.RemoteExecutionRequest{ID: "original-model", Binding: state.Binding, Target: "conversation", ExpectedRevision: c.view.Revision, Selection: productmanagement.Selection{Model: "public/model", Effort: "high"}}
	result, err := e.ChangeRemoteExecutionSettings(t.Context(), request)
	if err == nil || result.Outcome != "unknown" || e.Snapshot().CanSend {
		t.Fatal("unknown model change was not fenced", result, err)
	}
	journal, err := os.ReadFile(filepath.Join(e.root, "product-client-receipts.json"))
	if err != nil || strings.Contains(string(journal), "public/model") || !strings.Contains(string(journal), "configure-execution") {
		t.Fatal("model choices escaped digest-only receipt", err)
	}
	c.result = productrpc.Result{Outcome: "unknown"}
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := e.RemoteRuntime(t.Context())
	if err != nil || len(current.Pending) != 1 || current.Pending[0].ID != request.ID {
		t.Fatal(current, err)
	}
	view, err := e.RemoteExecutionSettings(t.Context(), current.Binding)
	if err != nil || view.Conversation.Effort != "high" || e.Snapshot().CanSend || e.Snapshot().Phase != "unknown" {
		t.Fatal("matching state inferred an original receipt", view, err)
	}
	request.ID = "replacement"
	request.Binding = current.Binding
	if _, err = e.ChangeRemoteExecutionSettings(t.Context(), request); err == nil || c.calls != 1 {
		t.Fatal("unknown model edit was replaced")
	}
	c.result = productrpc.Result{Outcome: "accepted"}
	if result, err = e.ReconcileRemoteManagement(t.Context(), current.Binding, "original-model"); err != nil || result.Outcome != "accepted" || len(e.receipts.Pending) != 0 || c.calls != 1 {
		t.Fatal("original receipt lookup replayed mutation", result, err)
	}
	if len(c.lookups) == 0 || c.lookups[len(c.lookups)-1] != "original-model" {
		t.Fatal(c.lookups)
	}
}
func TestRemoteExecutionForeignMutationReceiptStaysUnknown(t *testing.T) {
	e, c, state := executionEngine(t)
	c.change = func(command productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error) {
		return productmanagement.ExecutionResult{Scope: productmanagement.Scope{BotID: "foreign", Generation: "generation-one"}, ID: command.ID, Outcome: "accepted"}, nil
	}
	result, err := e.ChangeRemoteExecutionSettings(t.Context(), backend.RemoteExecutionRequest{ID: "owned-model", Binding: state.Binding, Target: "conversation", ExpectedRevision: c.view.Revision, Selection: c.view.Conversation})
	if err == nil || result.Outcome != "unknown" || len(e.receipts.Pending) != 1 {
		t.Fatal("foreign receipt cleared original intent", result, err)
	}
}

type executionSourceFixture struct {
	state productmanagement.ExecutionState
	calls int
}

func (s *executionSourceFixture) ReadModelSettings(context.Context) (productmanagement.ExecutionState, []api.ModelOption, error) {
	return s.state, []api.ModelOption{{Model: "public/model", Efforts: []string{"medium", "high"}}}, nil
}
func (s *executionSourceFixture) ApplyModelSettings(_ context.Context, revision, target string, selection productmanagement.Selection) error {
	if revision != productmanagement.ExecutionRevision(s.state) {
		return productmanagement.ErrExecutionConflict
	}
	s.calls++
	if target == "conversation" {
		s.state.Conversation.Model = selection.Model
		s.state.Conversation.Effort = selection.Effort
	} else if selection.Model == "" {
		s.state.Work = &api.WorkExecutionSettings{}
	} else {
		s.state.Work.Model = selection.Model
		s.state.Work.Effort = selection.Effort
	}
	return nil
}
func TestRemoteExecutionActualFramesWithoutHostManagement(t *testing.T) {
	port := &thinProductPort{snapshot: api.Snapshot{Connection: "ready", CanSend: true}}
	source := &executionSourceFixture{state: productmanagement.ExecutionState{Conversation: api.ExecutionSettings{Model: "public/model", Effort: "medium", ServiceTier: "priority", ApprovalMode: "manual"}, Work: &api.WorkExecutionSettings{Model: "public/model", Effort: "medium", ServiceTier: "priority"}}}
	token := strings.Repeat("synthetic-product-only-", 3)
	server, err := productrpc.NewServer(port, productrpc.Options{NodeID: "node-fixture", BotID: "bot-fixture", Token: token, JournalFile: filepath.Join(t.TempDir(), "journal.json"), Execution: func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
		return productmanagement.NewExecution(scope, source)
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
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	if err = e.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := e.RemoteRuntime(t.Context())
	if err != nil || !state.Available || !state.Capabilities.Execution || state.Capabilities.Configuration || state.Capabilities.Installation {
		t.Fatal(state, err)
	}
	view, err := e.RemoteExecutionSettings(t.Context(), state.Binding)
	if err != nil || view.Work == nil {
		t.Fatal(view, err)
	}
	result, err := e.ChangeRemoteExecutionSettings(t.Context(), backend.RemoteExecutionRequest{ID: "framed-model", Binding: state.Binding, Target: "conversation", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{Model: "public/model", Effort: "high"}})
	if err != nil || result.Outcome != "accepted" || source.calls != 1 || source.state.Conversation.ApprovalMode != "manual" || source.state.Conversation.ServiceTier != "priority" {
		t.Fatal("framed model preference changed protected native settings", result, err)
	}
	view, err = e.RemoteExecutionSettings(t.Context(), state.Binding)
	if err != nil {
		t.Fatal(err)
	}
	result, err = e.ChangeRemoteExecutionSettings(t.Context(), backend.RemoteExecutionRequest{ID: "framed-reset-work", Binding: state.Binding, Target: "work", ExpectedRevision: view.Revision, Selection: productmanagement.Selection{}})
	if err != nil || result.Outcome != "accepted" || source.calls != 2 || *source.state.Work != (api.WorkExecutionSettings{}) {
		t.Fatal("existing whole work inheritance reset changed", result, err)
	}
}
