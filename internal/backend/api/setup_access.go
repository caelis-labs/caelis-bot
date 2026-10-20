package api

import "context"

type setupAccessKey struct{}

// WithSetupAccess binds user-only setup reads and mutations to an authenticated
// surface. The ingress derives owner; it is never accepted from request JSON.
func WithSetupAccess(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, setupAccessKey{}, owner)
}
func SetupAccess(ctx context.Context) string {
	if owner, ok := ctx.Value(setupAccessKey{}).(string); ok {
		return owner
	}
	return "native"
}
