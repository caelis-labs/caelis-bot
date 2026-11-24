package caelis

import (
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Keep the service name stable for existing native task discovery and approvals.
const privateTasksService = "caelis_tasks"
const privateTasksPrefix = privateTasksService + "__"

func withResidentMCP(servers, resident []wire.SessionMCPServer) []wire.SessionMCPServer {
	servers = slices.DeleteFunc(slices.Clone(servers), func(server wire.SessionMCPServer) bool {
		return server.Name == privateTasksService
	})
	for _, server := range resident {
		if server.Name == privateTasksService {
			return append(servers, server)
		}
	}
	return servers
}

// manualMCPAction is read only when reconciling an original approval
// journal recorded by a previous Bot version.
type manualMCPAction struct {
	SessionID, PrincipalID, TurnID, RequestID, CallID, Name, Arguments string
}

func (a manualMCPAction) matchesReceipt(receipt wire.CommandResult) bool {
	return (value(receipt.SessionId) == "" || value(receipt.SessionId) == a.SessionID) &&
		(receipt.Target == nil || value(receipt.Target.TurnId) == "" || value(receipt.Target.TurnId) == a.TurnID)
}
