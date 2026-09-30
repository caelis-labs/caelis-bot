package productrpc

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend"
)

// ServicePort preserves the existing application assembly. Stop is the native
// owner's explicit shutdown; it is never invoked by a Client.Close.
type ServicePort struct {
	*backend.Service
	Stop func(context.Context) error
}

func (p ServicePort) InterruptTurn(ctx context.Context, expected string) error {
	if exact, ok := any(p.Service).(interface {
		InterruptTurn(context.Context, string) error
	}); ok {
		return exact.InterruptTurn(ctx, expected)
	}
	return ErrUnsupported
}

func (p ServicePort) ExactInterruptAvailable() bool {
	if exact, ok := any(p.Service).(interface{ ExactInterruptAvailable() bool }); ok {
		return exact.ExactInterruptAvailable()
	}
	return false
}

func (p ServicePort) StopBot(ctx context.Context) error {
	if p.Stop == nil {
		return ErrUnsupported
	}
	return p.Stop(ctx)
}

var _ Port = ServicePort{}
