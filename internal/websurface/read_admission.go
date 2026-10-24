package websurface

import "context"

type readAdmissionKey struct{}

// WithReadAdmissionRelease is used only by the host's HTTP drain gate. A fixed
// authenticated read action may release that write fence before native I/O.
// It does not change authorization or make an unconfirmed write terminal.
func WithReadAdmissionRelease(ctx context.Context, release func()) context.Context {
	return context.WithValue(ctx, readAdmissionKey{}, release)
}
func releaseReadAdmission(ctx context.Context) {
	if release, ok := ctx.Value(readAdmissionKey{}).(func()); ok {
		release()
	}
}
