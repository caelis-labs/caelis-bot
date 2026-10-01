package caelis

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// NodeRuntimeHealth separates an existing public Host's service handshake from
// the current configured provider's credential metadata. It asserts no owned
// lifetime, model execution, or authority to adopt/start/stop the Host.
type NodeRuntimeHealth struct{ HealthKnown, Healthy, AuthenticationKnown, Authenticated bool }

// InspectNodeRuntimeHealth observes only an existing discovery/Host. It never
// initializes a Store, starts a foreground, registers an application or changes
// model selection. A configured alternate cannot authenticate the current model.
func InspectNodeRuntimeHealth(ctx context.Context, settings api.RuntimeSettings) (out NodeRuntimeHealth, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	bounded, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, _, err = Discover(settings); err != nil {
		return out, nil
	}
	c, err := setupClient(bounded, settings)
	if err != nil {
		return out, readinessFailure("node-runtime-metadata-unavailable", err)
	}
	defer c.http.CloseIdleConnections()
	out.HealthKnown = true
	out.Healthy = true
	var current publicCurrentAuthentication
	_, err = inspectPublicReadiness(bounded, c, "", &current)
	if err != nil {
		return out, readinessFailure("node-runtime-metadata-unavailable", err)
	}
	out.AuthenticationKnown = current.known
	out.Authenticated = current.authenticated
	return out, nil
}
