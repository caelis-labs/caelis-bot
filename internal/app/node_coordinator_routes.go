package app

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

const nodeManagementDocumentLimit = 128 * 1024

func writeNodeManagementDocument(path string, doc nodeManagementDocument) error {
	if err := validateNodeCoordinatorRoutes(doc); err != nil {
		return err
	}
	// Match the loader and localstate's JSON encoder, including its newline.
	raw, err := json.Marshal(doc)
	if err != nil || len(raw)+1 > nodeManagementDocumentLimit {
		return errors.New("private node pairing document limit")
	}
	return localstate.Write(path, doc)
}

// Routes remain private and keyed by both enrolled identities. The registration
// SSH destination is the APP's management route and must never be overwritten.
type nodeCoordinatorSourceRoute struct {
	SourceNodeID, CoordinatorNodeID, SSHDestination string
}

func nodeCoordinatorRouteIDs(doc nodeManagementDocument) []string {
	ids := []string{api.LocalNodeID}
	for _, node := range doc.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func validateNodeCoordinatorRoutes(doc nodeManagementDocument) error {
	// At most 17 enrolled machines, with no self routes.
	if len(doc.SourceRoutes) > 17*16 {
		return errors.New("private coordinator source route limit")
	}
	known := map[string]bool{}
	for _, id := range nodeCoordinatorRouteIDs(doc) {
		known[id] = true
	}
	byCoordinator := map[string][]api.NodeCoordinatorSourceRoute{}
	for _, route := range doc.SourceRoutes {
		if !known[route.CoordinatorNodeID] {
			return errors.New("source route coordinator is not enrolled")
		}
		byCoordinator[route.CoordinatorNodeID] = append(byCoordinator[route.CoordinatorNodeID], api.NodeCoordinatorSourceRoute{SourceNodeID: route.SourceNodeID, SSHDestination: route.SSHDestination})
	}
	for coordinator, routes := range byCoordinator {
		if err := nodeplane.ValidateCoordinatorSourceRoutes(coordinator, nodeCoordinatorRouteIDs(doc), routes); err != nil {
			return err
		}
	}
	return nil
}

func nodeCoordinatorRoutes(doc nodeManagementDocument, coordinator string) []api.NodeCoordinatorSourceRoute {
	routes := []api.NodeCoordinatorSourceRoute{}
	for _, route := range doc.SourceRoutes {
		if route.CoordinatorNodeID == coordinator {
			routes = append(routes, api.NodeCoordinatorSourceRoute{SourceNodeID: route.SourceNodeID, SSHDestination: route.SSHDestination})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].SourceNodeID < routes[j].SourceNodeID })
	return routes
}

func nodeCoordinatorRouteEntriesEqual(a, b []api.NodeCoordinatorSourceRoute) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]api.NodeCoordinatorSourceRoute(nil), a...)
	b = append([]api.NodeCoordinatorSourceRoute(nil), b...)
	sort.Slice(a, func(i, j int) bool { return a[i].SourceNodeID < a[j].SourceNodeID })
	sort.Slice(b, func(i, j int) bool { return b[i].SourceNodeID < b[j].SourceNodeID })
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func replaceNodeCoordinatorRoutes(doc nodeManagementDocument, coordinator string, routes *[]api.NodeCoordinatorSourceRoute) (nodeManagementDocument, error) {
	if routes == nil {
		return doc, nil
	}
	known := false
	for _, id := range nodeCoordinatorRouteIDs(doc) {
		known = known || id == coordinator
	}
	if coordinator == "" || !known {
		return doc, errors.New("source routes require an enrolled coordinator")
	}
	if err := nodeplane.ValidateCoordinatorSourceRoutes(coordinator, nodeCoordinatorRouteIDs(doc), *routes); err != nil {
		return doc, err
	}
	next := make([]nodeCoordinatorSourceRoute, 0, len(doc.SourceRoutes)+len(*routes))
	for _, route := range doc.SourceRoutes {
		if route.CoordinatorNodeID != coordinator {
			next = append(next, route)
		}
	}
	for _, route := range *routes {
		next = append(next, nodeCoordinatorSourceRoute{SourceNodeID: route.SourceNodeID, CoordinatorNodeID: coordinator, SSHDestination: route.SSHDestination})
	}
	sort.Slice(next, func(i, j int) bool {
		if next[i].CoordinatorNodeID != next[j].CoordinatorNodeID {
			return next[i].CoordinatorNodeID < next[j].CoordinatorNodeID
		}
		return next[i].SourceNodeID < next[j].SourceNodeID
	})
	doc.SourceRoutes = next
	return doc, validateNodeCoordinatorRoutes(doc)
}
