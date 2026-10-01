package backend

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type coordinatorRouteStateFixture struct {
	NodeRoamingController
	state NodeRoamingState
}

func (f *coordinatorRouteStateFixture) NodeRoamingState(context.Context) (NodeRoamingState, error) {
	return f.state, nil
}

type coordinatorRouteManagementFixture struct {
	api.NodeManagementController
	catalog   api.NodeCatalog
	reads     int
	mutations []api.NodeCoordinatorSelection
}

func (f *coordinatorRouteManagementFixture) NodeCatalog(context.Context) (api.NodeCatalog, error) {
	f.reads++
	return f.catalog, nil
}

func (f *coordinatorRouteManagementFixture) SetNodeCoordinator(_ context.Context, request api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	f.mutations = append(f.mutations, request)
	return f.catalog, nil
}

func TestRoamingWrapperRejectsSameCoordinatorSourceRoutesWithoutIgnoringInput(t *testing.T) {
	for _, outcome := range []string{"enabled", "unknown"} {
		for _, count := range []int{0, 1} {
			t.Run(outcome+string(rune('0'+count)), func(t *testing.T) {
				management := &coordinatorRouteManagementFixture{catalog: api.NodeCatalog{Revision: "catalog-original"}}
				state := &coordinatorRouteStateFixture{state: NodeRoamingState{CoordinatorNodeID: "coordinator", Enabled: outcome == "enabled", Outcome: outcome}}
				wrapper := &nodeRoamingManagement{management, state}
				routes := []api.NodeCoordinatorSourceRoute{}
				if count == 1 {
					routes = append(routes, api.NodeCoordinatorSourceRoute{SourceNodeID: "source", SSHDestination: "existing-source-route"})
				}
				if _, err := wrapper.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator", ExpectedRevision: "catalog-original", SourceRoutes: &routes}); err == nil {
					t.Fatal("wrapper falsely accepted a route edit as an idempotent selection")
				}
				if management.reads != 0 || len(management.mutations) != 0 {
					t.Fatal("locked route edit reached management", management.reads, management.mutations)
				}
				if _, err := wrapper.SetNodeCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "coordinator", ExpectedRevision: "catalog-original"}); err != nil || management.reads != 1 || len(management.mutations) != 0 {
					t.Fatal("omitted routes lost same-coordinator idempotency", err)
				}
			})
		}
	}
}

func TestDisabledRoamingWrapperForwardsExplicitSourceRoutes(t *testing.T) {
	management := &coordinatorRouteManagementFixture{catalog: api.NodeCatalog{Revision: "catalog-original"}}
	wrapper := &nodeRoamingManagement{management, &coordinatorRouteStateFixture{state: NodeRoamingState{State: "disabled"}}}
	routes := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source", SSHDestination: "existing-source-route"}}
	request := api.NodeCoordinatorSelection{NodeID: "coordinator", ExpectedRevision: "catalog-original", SourceRoutes: &routes}
	if _, err := wrapper.SetNodeCoordinator(t.Context(), request); err != nil || len(management.mutations) != 1 || management.mutations[0].SourceRoutes != request.SourceRoutes {
		t.Fatal("wrapper discarded explicit source route input", management.mutations, err)
	}
}
