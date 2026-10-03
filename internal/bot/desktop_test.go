package bot

import (
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	"sync"
	"testing"
	"time"
)

type desktopFixture struct {
	mu                sync.Mutex
	calls, ends       int
	entered, canceled chan struct{}
}

func (*desktopFixture) Definitions() []api.ToolDefinition { return desktopcontrol.Definitions() }
func (d *desktopFixture) EndTurn(string)                  { d.mu.Lock(); d.ends++; d.mu.Unlock() }
func (d *desktopFixture) CallTool(ctx context.Context, name string, _ json.RawMessage) api.ToolResult {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	if name == "bot_desktop_act" && d.entered != nil {
		close(d.entered)
		<-ctx.Done()
		close(d.canceled)
	}
	return api.ToolResult{}
}
func TestDesktopTurnRevokesIdleAndRunningWork(t *testing.T) {
	r, _, _ := fixture(t)
	d := &desktopFixture{entered: make(chan struct{}), canceled: make(chan struct{})}
	r.ConfigureDesktopControl(d)
	r.BeginDesktopTurn()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.callLegacyTool(context.Background(), "bot_desktop_act", json.RawMessage(`{}`))
	}()
	<-d.entered
	r.StopDesktopTurn()
	select {
	case <-d.canceled:
	case <-time.After(time.Second):
		t.Fatal("input did not cancel")
	}
	<-done
	if !r.callLegacyTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)).IsError {
		t.Fatal("stopped turn remained active")
	}
	if r.callLegacyTool(t.Context(), "bot_desktop_reconcile", json.RawMessage(`{}`)).IsError {
		t.Fatal("recovery blocked after stop")
	}
	d.mu.Lock()
	ends := d.ends
	d.mu.Unlock()
	r.BeginDesktopTurn()
	r.StopDesktopTurn()
	d.mu.Lock()
	if d.ends <= ends {
		t.Fatal("idle grant not revoked")
	}
	d.mu.Unlock()
	r.Stop()
	r.BeginDesktopTurn()
	if !r.callLegacyTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)).IsError {
		t.Fatal("shutdown reactivated")
	}
}
func TestDesktopCaptureRequiresImageModelButSemanticInputDoesNot(t *testing.T) {
	r, _, _ := fixture(t)
	d := &desktopFixture{}
	r.ConfigureDesktopControl(d)
	r.BeginDesktopTurn()
	if !r.callLegacyTool(t.Context(), "bot_desktop_capture", json.RawMessage(`{}`)).IsError {
		t.Fatal("image support not required")
	}
	if d.calls != 0 {
		t.Fatal("unsupported image reached helper")
	}
	if r.callLegacyTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)).IsError || d.calls != 1 {
		t.Fatal("read unnecessarily depends on images")
	}
}
