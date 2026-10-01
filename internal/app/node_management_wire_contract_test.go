package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

type nodeWireExecutionEngine struct{ *controlLocalEngine }

func (*nodeWireExecutionEngine) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{{Model: "fixture-model", Efforts: []string{"medium", "high"}}}, nil
}
func (*nodeWireExecutionEngine) ExecutionOptions() api.ExecutionOptions {
	return api.ExecutionOptions{}
}
func (*nodeWireExecutionEngine) ChangeExecution(_ context.Context, _ api.ExecutionSettings, persist func() error) error {
	return persist()
}
func (*nodeWireExecutionEngine) ChangeWorkExecution(_ context.Context, _ api.WorkExecutionSettings, persist func() error) error {
	return persist()
}

type enrollmentWirePort struct{ productrpc.ServicePort }

func (enrollmentWirePort) Snapshot() api.Snapshot { return api.Snapshot{Connection: "ready"} }
func enrollmentWireClient(t *testing.T, a *Application) *productrpc.Client {
	t.Helper()
	server, err := productrpc.NewServer(enrollmentWirePort{productrpc.ServicePort{Service: a.Backend}}, productrpc.Options{NodeID: "fixture-owner", BotID: "fixture-bot", Token: strings.Repeat("f", 64), JournalFile: filepath.Join(t.TempDir(), "journal.json")})
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(server)
	t.Cleanup(host.Close)
	client, err := productrpc.NewClient(productrpc.ClientOptions{URL: host.URL, ExpectedNode: "fixture-owner", ExpectedBot: "fixture-bot", Token: strings.Repeat("f", 64)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	if _, err := client.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return client
}
func TestNodeConfigurationOfficialWireUsesActualServiceHashRevision(t *testing.T) {
	a := nativeManagementApplication(t)
	engine := &nodeWireExecutionEngine{&controlLocalEngine{}}
	a.Backend = backend.NewService(engine, nil, nil, nil, nil)
	a.Backend.ConfigureRuntime(filepath.Join(a.root, "runtime.json"), api.RuntimeSettings{Runtime: "codex"})
	a.Backend.ConfigureExecution(filepath.Join(a.root, "execution.json"), api.ExecutionSettings{Model: "fixture-model", Effort: "medium", ApprovalMode: "auto"})
	a.Backend.ConfigureWorkExecution(filepath.Join(a.root, "worker.json"), api.WorkExecutionSettings{Model: "fixture-model", Effort: "medium"})
	directory := filepath.Join(a.root, "local-agent")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	configuration := &nodeLocalCodexConfiguration{nodeLocalConfiguration{app: a, backend: api.NodeCodex}}
	local, err := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal, Configurations: map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCodex: configuration}})
	if err != nil {
		t.Fatal(err)
	}
	if err := AttachNodeManagement(a, NodeManagementNativeOptions{LocalAgent: local}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Backend.CloseNodeManagement(context.Background()) })
	client := enrollmentWireClient(t, a)
	for _, action := range []string{"conversation-model", "worker-model"} {
		view, err := client.NodeManagementState(t.Context(), productrpc.NodeQuery{Action: "configuration", NodeID: api.LocalNodeID, Backend: api.NodeCodex})
		if err != nil || view.Configuration == nil || len(view.Configuration.Guard.Revision) != 64 {
			t.Fatal("actual opaque revision unavailable", view, err)
		}
		request := api.NodeManagementRequest{Guard: view.Configuration.Guard, Ref: api.NodeOperationRef{NodeID: api.LocalNodeID, Backend: api.NodeCodex, OperationID: action + "-original"}, Change: &api.RuntimeConfigurationChange{Action: action, ExpectedRevision: view.Configuration.Guard.Revision, Selection: api.WorkExecutionSettings{Model: "fixture-model", Effort: "high"}}}
		request.Ref.RequestDigest, err = nodeplane.ManagementDigest(request)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.ManageNodes(t.Context(), request.Ref.OperationID, productrpc.NodeCommand{Action: "configure-node", Configuration: &request})
		if err != nil || result.Outcome != "accepted" || result.NodeManagement == nil || result.NodeManagement.Operation.Outcome != api.NodeCommitted {
			t.Fatal("Service contract rejected at wire", result, err)
		}
		receipt, err := client.NodeManagementState(t.Context(), productrpc.NodeQuery{Action: "operation", Operation: &request.Ref})
		if err != nil || receipt.Operation.Outcome != api.NodeCommitted {
			t.Fatal(receipt, err)
		}
		request.Ref.OperationID = action + "-stale"
		result, err = client.ManageNodes(t.Context(), request.Ref.OperationID, productrpc.NodeCommand{Action: "configure-node", Configuration: &request})
		if err != nil || result.Outcome != "rejected" || result.NodeManagement.Operation.Outcome != api.NodeConflicted {
			t.Fatal("stale guard accepted", result, err)
		}
	}
	conversation, err := a.Backend.ExecutionSettings()
	if err != nil || conversation.Effort != "high" || conversation.ApprovalMode != "auto" || a.Backend.WorkExecutionSettings().Effort != "high" {
		t.Fatal(conversation, err)
	}
}
