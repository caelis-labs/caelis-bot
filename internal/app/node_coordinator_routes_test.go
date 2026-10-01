package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

func coordinatorRoutesFixture(t *testing.T) *nativeNodeManagement {
	t.Helper()
	doc := nodeManagementDocument{Version: 1, Revision: 1, Coordinator: "coordinator-one"}
	for _, id := range []string{"source-one", "source-two", "coordinator-one", "coordinator-two"} {
		doc.Nodes = append(doc.Nodes, NodeRegistration{ID: id, Label: id, Join: api.NodeSSH, SSHDestination: "management-" + id, Directory: "/fixture/" + id, HelperPath: "/fixture/" + id + "/agent"})
	}
	n := &nativeNodeManagement{directory: t.TempDir(), local: singleNodeFixture(api.LocalNodeID), document: doc, clients: map[string]nodeplane.CatalogAgent{}, ownerCtx: context.Background()}
	n.options.Dial = func(_ context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		return singleNodeFixture(r.ID), nil
	}
	return n
}

func TestCoordinatorSourceRouteJSONPreservesOmissionAndExplicitClear(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		present   bool
		count     int
	}{
		{"omitted", `{"nodeId":"coordinator-one","expectedRevision":"r"}`, false, 0},
		{"null", `{"nodeId":"coordinator-one","expectedRevision":"r","sourceRoutes":null}`, false, 0},
		{"clear", `{"nodeId":"coordinator-one","expectedRevision":"r","sourceRoutes":[]}`, true, 0},
		{"replace", `{"nodeId":"coordinator-one","expectedRevision":"r","sourceRoutes":[{"sourceNodeId":"source-one","sshDestination":"source-existing-coordinator"}]}`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request api.NodeCoordinatorSelection
			if err := json.Unmarshal([]byte(tc.raw), &request); err != nil {
				t.Fatal(err)
			}
			if (request.SourceRoutes != nil) != tc.present || request.SourceRoutes != nil && len(*request.SourceRoutes) != tc.count {
				t.Fatal("omission/clear changed", request)
			}
		})
	}
}

func TestNativeCoordinatorSourceRoutesPersistPerPairWithoutChangingManagement(t *testing.T) {
	n := coordinatorRoutesFixture(t)
	m := NewNodeManagement(n, n)
	catalog, err := m.NodeCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := append([]NodeRegistration(nil), n.document.Nodes...)
	routes := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "source-one-existing-coordinator"}, {SourceNodeID: api.LocalNodeID, SSHDestination: "local-existing-coordinator"}}
	saved, err := m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-one", ExpectedRevision: catalog.Revision, SourceRoutes: &routes})
	if err != nil || saved.Revision == catalog.Revision || saved.ActiveBotNodeID != catalog.ActiveBotNodeID || !reflect.DeepEqual(saved.WorkerTarget, catalog.WorkerTarget) {
		t.Fatal("route save changed execution or lost its revision", saved, err)
	}
	if saved.Broker == nil || len(saved.Broker.SourceRoutes) != 2 || !reflect.DeepEqual(before, n.document.Nodes) {
		t.Fatal("route save overwrote management enrollment", saved)
	}
	// Inputs and returned presentation cannot mutate the private frozen records.
	routes[0].SSHDestination = "changed-caller-draft"
	saved.Broker.SourceRoutes[0].SSHDestination = "changed-view-draft"
	reloaded, err := loadNodeManagementDocument(filepath.Join(n.directory, "config.json"))
	if err != nil || !reflect.DeepEqual(n.document, reloaded) {
		t.Fatal("private restart lost source routes", err)
	}
	info, err := os.Stat(filepath.Join(n.directory, "config.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("source routes are not private", err)
	}
	// Choosing another coordinator and omitting routes preserves the first pair.
	other, err := m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-two", ExpectedRevision: saved.Revision})
	if err != nil || len(other.Broker.SourceRoutes) != 0 || len(n.document.SourceRoutes) != 2 {
		t.Fatal("coordinator switch discarded another coordinator's routes", other, err)
	}
	secondRoutes := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "source-one-second-coordinator"}}
	other, err = m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-two", ExpectedRevision: other.Revision, SourceRoutes: &secondRoutes})
	if err != nil || len(n.document.SourceRoutes) != 3 {
		t.Fatal("pair-keyed source route replaced a different coordinator", err)
	}
	empty := []api.NodeCoordinatorSourceRoute{}
	cleared, err := m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-two", ExpectedRevision: other.Revision, SourceRoutes: &empty})
	if err != nil || len(cleared.Broker.SourceRoutes) != 0 || len(n.document.SourceRoutes) != 2 {
		t.Fatal("explicit clear affected other coordinator routes", cleared, err)
	}
	returned, err := m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-one", ExpectedRevision: cleared.Revision})
	if err != nil || len(returned.Broker.SourceRoutes) != 2 || returned.Broker.SourceRoutes[1].SSHDestination != "source-one-existing-coordinator" {
		t.Fatal("omission did not preserve original source route", returned, err)
	}
}

func TestNativeCoordinatorSourceRouteInvalidRequestsPreservePrivateBytes(t *testing.T) {
	for _, tc := range []struct {
		name, coordinator string
		routes            []api.NodeCoordinatorSourceRoute
		stale             bool
	}{
		{"unknown-source", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "not-enrolled", SSHDestination: "existing-target"}}, false},
		{"self", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "coordinator-one", SSHDestination: "existing-target"}}, false},
		{"duplicate", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "one"}, {SourceNodeID: "source-one", SSHDestination: "two"}}, false},
		{"ssh-option", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "-oProxyCommand"}}, false},
		{"shell", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "alias; command"}}, false},
		{"empty-alias", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one"}}, false},
		{"long-alias", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: strings.Repeat("a", 257)}}, false},
		{"no-coordinator-clear", "", []api.NodeCoordinatorSourceRoute{}, false},
		{"unknown-coordinator", "not-enrolled", []api.NodeCoordinatorSourceRoute{}, false},
		{"stale-revision", "coordinator-one", []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "existing-target"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := coordinatorRoutesFixture(t)
			path := filepath.Join(n.directory, "config.json")
			if err := localstate.Write(path, n.document); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := n.Catalog(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if tc.stale {
				catalog.Revision = "stale"
			}
			if _, err := n.SetCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: tc.coordinator, ExpectedRevision: catalog.Revision, SourceRoutes: &tc.routes}); err == nil {
				t.Fatal("invalid source route admitted")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected request rewrote private pairing", err)
			}
		})
	}
}

func TestCoordinatorFacadeCannotAcceptDroppedSourceRouteInput(t *testing.T) {
	agent := newNodeManagementFixture()
	setup := &nodeSetupFixture{catalog: agent.catalog}
	m := NewNodeManagement(agent, setup)
	routes := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "other", SSHDestination: "source-existing-local"}}
	if _, err := m.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: api.LocalNodeID, ExpectedRevision: agent.catalog.Revision, SourceRoutes: &routes}); err == nil {
		t.Fatal("settings accepted a coordinator response that discarded source routes")
	}
}

func TestNativeCoordinatorRoutesRejectCorruptPrivatePairOnRestart(t *testing.T) {
	n := coordinatorRoutesFixture(t)
	n.document.SourceRoutes = []nodeCoordinatorSourceRoute{{SourceNodeID: "source-one", CoordinatorNodeID: "missing-coordinator", SSHDestination: "existing-target"}}
	path := filepath.Join(n.directory, "config.json")
	if err := localstate.Write(path, n.document); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadNodeManagementDocument(path); err == nil {
		t.Fatal("unbound coordinator route became trusted on restart")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("corrupt route metadata was adopted or rewritten", err)
	}
}

func TestPrivateCoordinatorRouteWriterRejectsOrphanAndOversizeWithoutReplacement(t *testing.T) {
	for _, reason := range []string{"removed-source", "removed-coordinator", "serialized-capacity"} {
		t.Run(reason, func(t *testing.T) {
			n := coordinatorRoutesFixture(t)
			n.document.SourceRoutes = []nodeCoordinatorSourceRoute{{SourceNodeID: "source-one", CoordinatorNodeID: "coordinator-one", SSHDestination: "existing-target"}}
			path := filepath.Join(n.directory, "config.json")
			if err := writeNodeManagementDocument(path, n.document); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			next := n.document
			next.Nodes = append([]NodeRegistration(nil), n.document.Nodes...)
			if reason == "serialized-capacity" {
				next.Nodes[0].Directory = "/" + strings.Repeat("a", nodeManagementDocumentLimit)
			} else {
				removed := "source-one"
				if reason == "removed-coordinator" {
					removed = "coordinator-one"
				}
				next.Nodes = nil
				for _, node := range n.document.Nodes {
					if node.ID != removed {
						next.Nodes = append(next.Nodes, node)
					}
				}
			}
			if err := writeNodeManagementDocument(path, next); err == nil {
				t.Fatal("writer persisted an unloadable pairing document")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid replacement changed private pairing bytes", err)
			}
		})
	}
}

type coordinatorRouteFenceCatalog struct {
	*nodeManagementFixture
	inspect func()
}

func (f *coordinatorRouteFenceCatalog) Catalog(ctx context.Context) (api.NodeCatalog, error) {
	f.inspect()
	return f.nodeManagementFixture.Catalog(ctx)
}

func TestNativeCoordinatorRouteWriteHoldsRoamingOperationFence(t *testing.T) {
	f := roamingControlFixture(t)
	n := coordinatorRoutesFixture(t)
	n.app = f.a
	n.options.ExecutionState = func() (string, *api.WorkTarget) { return api.LocalNodeID, nil }
	inspections := 0
	routes := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source-one", SSHDestination: "existing-target"}}
	// Capture revision before the write; Catalog itself is passive/unfenced.
	catalog, err := n.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	n.local = &coordinatorRouteFenceCatalog{singleNodeFixture(api.LocalNodeID), func() {
		inspections++
		if f.c.op.TryLock() {
			f.c.op.Unlock()
			t.Error("coordinator route write allowed Enable to interleave")
		}
	}}
	if _, err := n.SetCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator-one", ExpectedRevision: catalog.Revision, SourceRoutes: &routes}); err != nil {
		t.Fatal(err)
	}
	if inspections != 2 {
		t.Fatal("write did not hold the fence through both catalog reads", inspections)
	}
}

func TestNativeSameCoordinatorRouteEditBlockedBeforeCatalogWhenRoamingLocked(t *testing.T) {
	for _, state := range []string{"enabled", "unknown", "enabling", "disabling", "pending-restore"} {
		t.Run(state, func(t *testing.T) {
			f := roamingControlFixture(t)
			f.c.mu.Lock()
			f.c.state.CoordinatorNodeID = "local"
			f.c.state.Enabled = state == "enabled"
			if state == "unknown" {
				f.c.state.Outcome = "unknown"
			}
			f.c.state.State = state
			f.c.pendingLocalRestore = state == "pending-restore"
			f.c.mu.Unlock()
			n := &nativeNodeManagement{app: f.a}
			clear := []api.NodeCoordinatorSourceRoute{}
			if _, err := n.SetCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "local", SourceRoutes: &clear}); err == nil || !strings.Contains(err.Error(), "disable automatic roaming") {
				t.Fatal("same-coordinator route edit bypassed native ownership guard", err)
			}
		})
	}
}
