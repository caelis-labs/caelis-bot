package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type HostIdentity struct {
	Version, Build, Instance string
}

// InspectHost reads the actual authenticated Host, not the invoking CLI's pin.
// It never registers a Bot, resumes a Session or dispatches an operation.
func InspectHost(ctx context.Context, settings api.RuntimeSettings) (HostIdentity, error) {
	d, token, err := Discover(settings)
	if err != nil {
		return HostIdentity{}, err
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		return HostIdentity{}, err
	}
	defer c.http.CloseIdleConnections()
	info, err := initialize(ctx, c)
	if err != nil {
		return HostIdentity{}, err
	}
	if value(info.InstanceId) != d.InstanceID || value(info.DistributionVersion) == "" || value(info.BuildId) == "" {
		return HostIdentity{}, errors.New("runtime identity unconfirmed")
	}
	return HostIdentity{value(info.DistributionVersion), value(info.BuildId), value(info.InstanceId)}, nil
}
