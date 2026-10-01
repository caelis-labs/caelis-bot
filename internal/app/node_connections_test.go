package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type nodeConnectionFixture struct {
	*nodeManagementFixture
	calls         []string
	refs          []api.NodeRuntimeConnectionRef
	input         api.RuntimeConnectionInput
	changedTarget bool
	unknownStop   bool
}

func (f *nodeConnectionFixture) record(action string, ref api.NodeRuntimeConnectionRef) {
	f.calls = append(f.calls, action)
	f.refs = append(f.refs, ref)
}
func (f *nodeConnectionFixture) BeginNodeRuntimeConnection(_ context.Context, g api.NodeEditGuard, id string) (api.NodeRuntimeConnectionRef, error) {
	ref := api.NodeRuntimeConnectionRef{NodeID: g.NodeID, Backend: g.Backend, OperationID: id}
	f.record("begin", ref)
	if f.changedTarget {
		ref.NodeID = "replacement-target"
	}
	return ref, nil
}
func (f *nodeConnectionFixture) NodeRuntimeConnectionCatalog(_ context.Context, r api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	f.record("catalog", r)
	return api.RuntimeConnectionCatalog{Choices: []api.RuntimeConnectChoice{{ID: kind}}}, nil
}
func (f *nodeConnectionFixture) NodeRuntimeSetupCatalog(_ context.Context, r api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	f.record("setup-catalog", r)
	return []api.SetupChoice{{Value: provider}}, nil
}
func (f *nodeConnectionFixture) StartNodeRuntimeConnection(_ context.Context, r api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	f.record("start", r)
	f.input = input
	return api.RuntimeFlow{ID: "original-flow", Revision: "native-revision", Stage: "authorization"}, nil
}
func (f *nodeConnectionFixture) AdvanceNodeRuntimeConnection(_ context.Context, r api.NodeRuntimeConnectionRef, a api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	f.record("advance", r)
	return api.RuntimeFlow{ID: a.ID, Revision: a.Revision, Stage: "models"}, nil
}
func (f *nodeConnectionFixture) WaitNodeRuntimeConnection(_ context.Context, r api.NodeRuntimeConnectionRef, id string, after int) (api.RuntimeFlow, error) {
	f.record("wait", r)
	return api.RuntimeFlow{ID: id, Stage: "complete", Sequence: after + 1}, nil
}
func (f *nodeConnectionFixture) CancelNodeRuntimeConnection(_ context.Context, r api.NodeRuntimeConnectionRef, id string) error {
	f.record("cancel", r)
	return nil
}
func (f *nodeConnectionFixture) CloseNodeRuntimeConnection(_ context.Context, r api.NodeRuntimeConnectionRef) error {
	f.record("close", r)
	if f.unknownStop {
		return errors.New("original owned setup stop unconfirmed")
	}
	return nil
}

type nodeConnectionRoamingFixture struct{ backend.NodeRoamingController }

func nodeConnectionBridge(t *testing.T) (*Application, *nodeConnectionFixture, *nodeConnectionFixture) {
	t.Helper()
	a := nativeManagementApplication(t)
	local := &nodeConnectionFixture{nodeManagementFixture: singleNodeFixture(api.LocalNodeID)}
	target := &nodeConnectionFixture{nodeManagementFixture: singleNodeFixture("target")}
	native := &nativeNodeManagement{app: a, local: local, options: NodeManagementNativeOptions{ExecutionState: func() (string, *api.WorkTarget) { return "", nil }}, document: nodeManagementDocument{Version: 1, Nodes: []NodeRegistration{{ID: "target", Label: "Target", Join: api.NodeSSH, Directory: "/private/target", SSHDestination: "existing-target", HelperPath: "/paired/helper"}}}, clients: map[string]nodeplane.CatalogAgent{"target": target}}
	a.Backend.SetNodeManagementController(NewNodeManagement(native, native))
	if e := a.Backend.ConfigureNodeRoaming(&nodeConnectionRoamingFixture{}); e != nil {
		t.Fatal(e)
	}
	return a, local, target
}

func TestNodeConnectionBridgeKeepsOriginalTargetAcrossSelectionAndRoamingFacade(t *testing.T) {
	a, local, target := nodeConnectionBridge(t)
	ctx := t.Context()
	read, e := a.Backend.NodeRuntimeConfiguration(ctx, "target", api.NodeCaelis)
	if e != nil || len(target.calls) != 0 || len(local.calls) != 0 {
		t.Fatal("passive settings read dispatched setup", read, e)
	}
	ref, e := a.Backend.BeginNodeRuntimeConnection(ctx, read.Guard, "original-connection")
	if e != nil || ref != (api.NodeRuntimeConnectionRef{NodeID: "target", Backend: api.NodeCaelis, OperationID: "original-connection"}) {
		t.Fatal("original begin did not retain exact pairing", ref, e)
	}
	catalog, e := a.Backend.NodeCatalog(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Backend.SelectNode(ctx, api.LocalNodeID, catalog.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Backend.NodeRuntimeConnectionCatalog(ctx, ref, "api-key"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Backend.NodeRuntimeSetupCatalog(ctx, ref, "models", "native-provider", "https://public-provider.invalid"); e != nil {
		t.Fatal(e)
	}
	flow, e := a.Backend.StartNodeRuntimeConnection(ctx, ref, api.RuntimeConnectionInput{Kind: "api-key", Choice: "native-provider", APIKey: "SYNTHETIC_MANUAL_INPUT_ONLY"})
	if e != nil || flow.ID != "original-flow" || target.input.APIKey != "SYNTHETIC_MANUAL_INPUT_ONLY" {
		t.Fatal("manual input lost native target SDK boundary", flow, e)
	}
	if _, e = a.Backend.AdvanceNodeRuntimeConnection(ctx, ref, api.RuntimeFlowAction{ID: flow.ID, Revision: flow.Revision, Action: "select-model", Input: api.RuntimeFlowInput{Model: "target-model"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Backend.WaitNodeRuntimeConnection(ctx, ref, flow.ID, 1); e != nil {
		t.Fatal(e)
	}
	if e = a.Backend.CancelNodeRuntimeConnection(ctx, ref, flow.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.Backend.CloseNodeRuntimeConnection(ctx, ref); e != nil {
		t.Fatal(e)
	}
	if len(local.calls) != 0 || strings.Join(target.calls, ",") != "begin,catalog,setup-catalog,start,advance,wait,cancel,close" {
		t.Fatal("selection rerouted original settings interaction", local.calls, target.calls)
	}
	for _, r := range target.refs {
		if r != ref {
			t.Fatal("original native setup scope changed", r)
		}
	}
	if a.sourceRetired || a.started {
		t.Fatal("settings interaction changed source execution")
	}
	if e = filepath.WalkDir(a.root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if strings.Contains(string(b), target.input.APIKey) {
				t.Fatal("manual input reached APP journal", path)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestNodeConnectionBridgeRejectsStaleGuardPathsRetargetingAndUnknownCleanup(t *testing.T) {
	a, _, target := nodeConnectionBridge(t)
	ctx := t.Context()
	g := api.NodeEditGuard{NodeID: "target", Backend: api.NodeCaelis, Revision: "stale"}
	if _, e := a.Backend.BeginNodeRuntimeConnection(ctx, g, "original"); e == nil || len(target.calls) != 0 {
		t.Fatal("stale begin reached target", e)
	}
	g.Revision = "7"
	ref, e := a.Backend.BeginNodeRuntimeConnection(ctx, g, "original")
	if e != nil {
		t.Fatal(e)
	}
	before := len(target.calls)
	if _, e = a.Backend.StartNodeRuntimeConnection(ctx, ref, api.RuntimeConnectionInput{Settings: &api.RuntimeSettings{Runtime: "caelis", CaelisStore: "/renderer/path"}}); e == nil || len(target.calls) != before {
		t.Fatal("renderer selected target native Store", e)
	}
	foreign := ref
	foreign.NodeID = "replacement-target"
	if _, e = a.Backend.NodeRuntimeConnectionCatalog(ctx, foreign, "account"); e == nil || len(target.calls) != before {
		t.Fatal("unregistered replacement target accepted", e)
	}
	foreign = ref
	foreign.Backend = api.NodeCodex
	if e = a.Backend.CloseNodeRuntimeConnection(ctx, foreign); e == nil || len(target.calls) != before {
		t.Fatal("connection cleanup changed backend", e)
	}
	target.changedTarget = true
	if _, e = a.Backend.BeginNodeRuntimeConnection(ctx, g, "replacement"); e == nil {
		t.Fatal("paired response retargeting accepted")
	}
	target.unknownStop = true
	if e = a.Backend.CloseNodeRuntimeConnection(ctx, ref); e == nil || !strings.Contains(e.Error(), "unconfirmed") {
		t.Fatal("unconfirmed target cleanup hidden", e)
	}
}
