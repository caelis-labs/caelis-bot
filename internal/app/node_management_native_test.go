package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func nativeManagementApplication(t *testing.T) *Application {
	t.Helper()
	return &Application{root: t.TempDir(), Backend: backend.NewService(nil, nil, nil, nil, nil)}
}

func singleNodeFixture(id string) *nodeManagementFixture {
	f := newNodeManagementFixture()
	f.catalog.Nodes = f.catalog.Nodes[:1]
	f.catalog.Nodes[0].ID = id
	f.catalog.SelectedNodeID = id
	f.catalog.ActiveBotNodeID = ""
	f.catalog.WorkerTarget = nil
	return f
}

func TestNativeNodeEnrollmentPersistsIdentityAndExactReceiptsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	a := nativeManagementApplication(t)
	local := singleNodeFixture("local")
	remote := singleNodeFixture("enrolled-node")
	dialed := []NodeRegistration{}
	o := NodeManagementNativeOptions{LocalAgent: local, Bootstrap: func(_ context.Context, r api.NodeAddRequest) (NodeRegistration, error) {
		return NodeRegistration{ID: "enrolled-node", Label: r.Label, Join: api.NodeSSH, SSHDestination: r.SSHDestination, Directory: "/home/owner/.agent", HelperPath: "/home/owner/.agent/helper"}, nil
	}, Dial: func(_ context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		dialed = append(dialed, r)
		return remote, nil
	}}
	if err := AttachNodeManagement(a, o); err != nil {
		t.Fatal(err)
	}
	initial, err := a.Backend.NodeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	added, err := a.Backend.AddNode(ctx, api.NodeAddRequest{Label: "Work machine", Join: api.NodeSSH, SSHDestination: "existing-user@host", ExpectedRevision: initial.Revision})
	if err != nil || added.Node.ID != "enrolled-node" {
		t.Fatal("verified enrollment failed", added, err)
	}
	c, err := a.Backend.NodeCatalog(ctx)
	if err != nil || len(c.Nodes) != 2 {
		t.Fatal(c, err)
	}
	if _, err := a.Backend.SelectNode(ctx, "enrolled-node", c.Revision); err != nil {
		t.Fatal(err)
	}
	intent := nodeIntent(t, "enrolled-node", "remote-original")
	r, err := a.Backend.ChangeNodeConfiguration(ctx, intent)
	if err != nil || r.Outcome != api.NodeUnknown {
		t.Fatal(r, err)
	}
	if err := a.Backend.CloseNodeManagement(ctx); err != nil {
		t.Fatal(err)
	}
	// A fresh APP facade reloads the private pairing and queries only the
	// original target. No native mutation is resent to reconstruct its receipt.
	b := &Application{root: a.root, Backend: backend.NewService(nil, nil, nil, nil, nil)}
	if err := AttachNodeManagement(b, o); err != nil {
		t.Fatal(err)
	}
	defer b.Backend.CloseNodeManagement(ctx)
	recovered, err := b.Backend.ReconcileNodeOperation(ctx, intent.Ref)
	if err != nil || recovered.Outcome != api.NodeCommitted || recovered.Ref != intent.Ref {
		t.Fatal("restart changed original target", recovered, err)
	}
	if len(dialed) != 2 || dialed[1].ID != "enrolled-node" || dialed[1].SSHDestination != "existing-user@host" {
		t.Fatal("private pairing lost", dialed)
	}
	remote.mu.Lock()
	sends, queries := remote.manageCalls, remote.reconcileCalls
	remote.mu.Unlock()
	if sends != 1 || queries != 1 {
		t.Fatal("unknown mutation resent across APP restart", sends, queries)
	}
	info, err := os.Stat(filepath.Join(a.root, "nodeplane", "config.json"))
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("pairing is not private", err)
	}
}

func TestNativeNodeBootstrapIdentityMismatchCannotBecomeEnrollment(t *testing.T) {
	ctx := context.Background()
	a := nativeManagementApplication(t)
	o := NodeManagementNativeOptions{LocalAgent: singleNodeFixture("local"), Bootstrap: func(_ context.Context, r api.NodeAddRequest) (NodeRegistration, error) {
		return NodeRegistration{ID: "expected-node", Label: r.Label, Join: api.NodeSSH, SSHDestination: r.SSHDestination, Directory: "/private/native", HelperPath: "/private/native/agent"}, nil
	}, Dial: func(context.Context, NodeRegistration) (nodeplane.CatalogAgent, error) {
		return singleNodeFixture("different-node"), nil
	}}
	if err := AttachNodeManagement(a, o); err != nil {
		t.Fatal(err)
	}
	defer a.Backend.CloseNodeManagement(ctx)
	c, err := a.Backend.NodeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.AddNode(ctx, api.NodeAddRequest{Label: "Machine", Join: api.NodeSSH, SSHDestination: "existing-host", ExpectedRevision: c.Revision}); err == nil {
		t.Fatal("wrong native identity became enrolled")
	}
	if _, err := os.Stat(filepath.Join(a.root, "nodeplane", "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed identity probe published pairing", err)
	}
}

func TestNativeOutgoingRequiresRealJoinPlanAndPreservesBrokerPairing(t *testing.T) {
	ctx := context.Background()
	a := nativeManagementApplication(t)
	local := singleNodeFixture("local")
	var seen NodeRegistration
	o := NodeManagementNativeOptions{LocalAgent: local, PrepareOutgoing: func(_ context.Context, r NodeRegistration) (NodeRegistration, api.NodeJoinInstructions, error) {
		r.BrokerNodeID = "local"
		r.SocketPath = filepath.Join(t.TempDir(), "agent.sock")
		return r, api.NodeJoinInstructions{NodeID: r.ID, State: api.NodeJoinWaiting, Instructions: "Run the paired foreground SSH join."}, nil
	}, Dial: func(_ context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		seen = r
		return singleNodeFixture(r.ID), nil
	}}
	if err := AttachNodeManagement(a, o); err != nil {
		t.Fatal(err)
	}
	defer a.Backend.CloseNodeManagement(ctx)
	c, err := a.Backend.NodeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Backend.AddNode(ctx, api.NodeAddRequest{Label: "NAT machine", Join: api.NodeOutgoing, ExpectedRevision: c.Revision})
	if err != nil || result.JoinInstructions == nil || result.JoinInstructions.State != api.NodeJoinWaiting {
		t.Fatal(result, err)
	}
	if _, err := a.Backend.DetectNode(ctx, result.Node.ID); err != nil {
		t.Fatal(err)
	}
	if seen.ID != result.Node.ID || seen.BrokerNodeID != "local" || seen.Join != api.NodeOutgoing {
		t.Fatal("outgoing identity/gateway was not retained", seen)
	}
	// No native join plan means no permanent fabricated waiting enrollment.
	b := nativeManagementApplication(t)
	if err := AttachNodeManagement(b, NodeManagementNativeOptions{LocalAgent: local}); err != nil {
		t.Fatal(err)
	}
	defer b.Backend.CloseNodeManagement(ctx)
	c, err = b.Backend.NodeCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Backend.AddNode(ctx, api.NodeAddRequest{Label: "NAT machine", Join: api.NodeOutgoing, ExpectedRevision: c.Revision}); err == nil {
		t.Fatal("unconfigured outgoing path advertised joining")
	}
}

func TestNativePairingCorruptionPreservesSource(t *testing.T) {
	a := nativeManagementApplication(t)
	dir := filepath.Join(a.root, "nodeplane")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	source := []byte(`{"Version":1,"Revision":1,"Nodes":[],"Coordinator":"not-enrolled"}`)
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := AttachNodeManagement(a, NodeManagementNativeOptions{LocalAgent: singleNodeFixture("local")}); err == nil {
		t.Fatal("corrupt pairing silently replaced")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(source) {
		t.Fatal("corrupt source overwritten", err)
	}
	if strings.Contains(string(got), "operationId") {
		t.Fatal("unrelated native receipts entered pairing document")
	}
}

type thinNodeScopeTrap struct {
	api.Engine
	remoteCalls atomic.Int32
}

func (*thinNodeScopeTrap) ProviderInfo() api.ProviderInfo { return api.ProviderInfo{ID: "codex"} }
func (e *thinNodeScopeTrap) Models(context.Context) ([]api.ModelOption, error) {
	e.remoteCalls.Add(1)
	return nil, errors.New("remote model catalog reached by local node")
}
func (*thinNodeScopeTrap) ExecutionOptions() api.ExecutionOptions { return api.ExecutionOptions{} }
func (e *thinNodeScopeTrap) CurrentExecutionSettings(context.Context) (api.ExecutionSettings, error) {
	e.remoteCalls.Add(1)
	return api.ExecutionSettings{Model: "remote-only-model"}, nil
}
func (e *thinNodeScopeTrap) ChangeExecution(context.Context, api.ExecutionSettings, func() error) error {
	e.remoteCalls.Add(1)
	return errors.New("remote mutation reached by local node")
}

func TestThinNodeManagementLocalModelsNeverUseRemoteBotBackend(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture unavailable")
	}
	a := nativeManagementApplication(t)
	remote := &thinNodeScopeTrap{}
	a.Backend = backend.NewService(remote, nil, nil, nil, nil)
	a.product = &productEngine{pairing: thinPairing()}
	body, err := os.ReadFile("../nodeagent/testdata/codex_setup_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	body = append([]byte("#!"+python+"\n"), body[strings.Index(string(body), "\n")+1:]...)
	binary := filepath.Join(t.TempDir(), "local-codex-fixture")
	if err = os.WriteFile(binary, body, 0700); err != nil {
		t.Fatal(err)
	}
	if err = AttachNodeManagement(a, NodeManagementNativeOptions{LocalCodexBinary: binary}); err != nil {
		t.Fatal(err)
	}
	defer a.Backend.CloseNodeManagement(t.Context())
	catalog, err := a.Backend.NodeCatalog(t.Context())
	if err != nil || catalog.ActiveBotNodeID != a.product.pairing.NodeID || catalog.WorkerTarget != nil {
		t.Fatal("thin APP invented local execution owner", catalog, err)
	}
	paired := false
	for _, node := range catalog.Nodes {
		if node.ID == a.product.pairing.NodeID {
			paired = node.OS == api.NodeOSUnknown && len(node.Runtimes) == 0
		}
	}
	if !paired {
		t.Fatal("paired remote owner metadata absent or invented")
	}
	if catalog.PairedRuntime == nil || catalog.PairedRuntime.NodeID != a.product.pairing.NodeID || catalog.PairedRuntime.Binding != "" {
		t.Fatal("offline thin APP invented a usable product binding")
	}
	view, err := a.Backend.NodeRuntimeConfiguration(t.Context(), "local", api.NodeCodex)
	if err != nil || !view.ConfigurationAvailable || view.Conversation == nil || view.Conversation.Model != "" {
		t.Fatal("independent local configuration unavailable", view, err)
	}
	r := api.NodeManagementRequest{Guard: view.Guard, Ref: api.NodeOperationRef{NodeID: "local", Backend: api.NodeCodex, OperationID: "local-thin-setting"}, Change: &api.RuntimeConfigurationChange{Action: "conversation-model", ExpectedRevision: view.Guard.Revision, Selection: api.WorkExecutionSettings{Model: "fixture-model", Effort: "high"}}}
	r.Ref.RequestDigest, err = nodeplane.ManagementDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Backend.ChangeNodeConfiguration(t.Context(), r)
	if err != nil || result.Outcome != api.NodeCommitted {
		t.Fatal("local target preference update unavailable", result, err)
	}
	updated, err := a.Backend.NodeRuntimeConfiguration(t.Context(), "local", api.NodeCodex)
	if err != nil || updated.Conversation.Model != "fixture-model" {
		t.Fatal("local setting did not apply to local native profile", updated, err)
	}
	if remote.remoteCalls.Load() != 0 {
		t.Fatal("thin local management crossed into remote Bot", remote.remoteCalls.Load())
	}
	if _, err := a.Backend.NodeRuntimeConfiguration(t.Context(), a.product.pairing.NodeID, api.NodeCodex); err == nil {
		t.Fatal("read-only product pairing became an enrolled management agent")
	}
	a.product.mu.Lock()
	a.product.connection = "ready"
	a.product.client = newThinClientFixture()
	a.product.identity = productrpc.Identity{Scope: productrpc.Scope{BotID: a.product.pairing.BotID, Generation: "native-generation"}, Capabilities: productrpc.Capabilities{RuntimeManagement: true}}
	a.product.mu.Unlock()
	ready, err := a.Backend.NodeCatalog(t.Context())
	if err != nil || ready.PairedRuntime == nil || ready.PairedRuntime.Binding == "" {
		t.Fatal("ready native binding absent", err)
	}
	a.product.mu.Lock()
	a.product.attempt++
	a.product.mu.Unlock()
	reconnected, err := a.Backend.NodeCatalog(t.Context())
	if err != nil || reconnected.PairedRuntime.Binding == ready.PairedRuntime.Binding || reconnected.Revision == ready.Revision {
		t.Fatal("new native binding retained stale view guard", err)
	}
	if remote.remoteCalls.Load() != 0 {
		t.Fatal("reading paired native binding queried remote execution", remote.remoteCalls.Load())
	}
}
