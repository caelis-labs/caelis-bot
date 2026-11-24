package caelis

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/testsupport/hostfixture"
)

type nativeHostFixture struct {
	*hostfixture.Store
	settings api.RuntimeSettings
}

func newNativeHostFixture(t *testing.T, bin string) *nativeHostFixture {
	t.Helper()
	store := hostfixture.New(t, bin, "store")
	store.IsolateRecoveryEnvironment(t)
	return &nativeHostFixture{store, api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: store.Path}}
}

func (h *nativeHostFixture) start(t *testing.T, ctx context.Context) {
	t.Helper()
	started := time.Now()
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pid, err := h.startReady(ready)
	if err != nil {
		t.Fatalf("private Host readiness (PID %d): %v", pid, err)
	}
	t.Logf("private foreground Host PID %d ready in %s", pid, time.Since(started).Round(time.Millisecond))
}

func (h *nativeHostFixture) startReady(ctx context.Context) (int, error) {
	p, err := h.StartForeground(ctx)
	if err != nil {
		return 0, err
	}
	return p.PID, h.waitReady(ctx, p)
}

func (h *nativeHostFixture) waitReady(ctx context.Context, p *hostfixture.Process) error {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var last error
	for {
		d, token, err := Discover(h.settings)
		if err == nil {
			var c *client
			c, err = newClient(d.Endpoint, token)
			if err == nil {
				info, initErr := initialize(ctx, c)
				c.http.CloseIdleConnections()
				err = initErr
				if err == nil && value(info.InstanceId) == d.InstanceID && !p.Exited() {
					return nil
				}
			}
		}
		last = err
		select {
		case <-p.Done():
			err := p.Wait(ctx)
			return fmt.Errorf("foreground Host exited before readiness (exit error: %v)", err)
		case <-ctx.Done():
			return errors.Join(ctx.Err(), last)
		case <-tick.C:
		}
	}
}

func (h *nativeHostFixture) stopForeground(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.StopForeground(ctx); err != nil {
		t.Fatal(err)
	}
}

func closeNativeFixture(t *testing.T, resource interface{ Close(context.Context) error }) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := resource.Close(ctx); err != nil {
		t.Errorf("close private Host observer: %v", err)
	}
}

func registerNativeFixture(t *testing.T, resource interface{ Close(context.Context) error }) {
	t.Helper()
	t.Cleanup(func() { closeNativeFixture(t, resource) })
}

func closeNativeHTTP(server *httptest.Server) {
	server.CloseClientConnections()
	server.Close()
}
