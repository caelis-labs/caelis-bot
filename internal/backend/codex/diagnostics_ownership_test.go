package codex

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

type diagnosticConnection struct{ commands int }

func (c *diagnosticConnection) Read([]byte) (int, error)         { c.commands++; return 0, io.EOF }
func (c *diagnosticConnection) Write(b []byte) (int, error)      { c.commands++; return len(b), nil }
func (c *diagnosticConnection) Close() error                     { c.commands++; return nil }
func (c *diagnosticConnection) SetWriteDeadline(time.Time) error { c.commands++; return nil }

type diagnosticOwner struct{ *diagnosticConnection }

func (c *diagnosticOwner) freezeOwned() error    { c.commands++; return nil }
func (c *diagnosticOwner) forceKillOwned() error { c.commands++; return nil }

type diagnosticEndpoint struct{ *diagnosticOwner }

func (*diagnosticEndpoint) terminalEndpoint() string { return "unix:///PRIVATE_ENDPOINT_SENTINEL" }

func TestDiagnosticsProjectsRetainedOwnershipAndIndependentTerminalCapability(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		owner, endpoint, stop   bool
		closed, closing         bool
		disconnected            bool
		wantOwned, wantTerminal bool
	}{
		{name: "owned attachable", owner: true, endpoint: true, stop: true, wantOwned: true, wantTerminal: true},
		{name: "owned stdio", owner: true, stop: true, wantOwned: true},
		{name: "shared endpoint", endpoint: true, wantTerminal: true},
		{name: "endpoint and fencing interfaces without retained stop", owner: true, endpoint: true, wantTerminal: true},
		{name: "stop without fencing", stop: true},
		{name: "closed owner", owner: true, endpoint: true, stop: true, closed: true},
		{name: "closing owner", owner: true, endpoint: true, stop: true, closing: true},
		{name: "disconnected owner", owner: true, endpoint: true, stop: true, disconnected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &diagnosticConnection{}
			owned := &diagnosticOwner{base}
			var conn connection = base
			if tc.owner {
				conn = owned
			}
			if tc.endpoint {
				// The absence of a retained stop handle still makes this shared.
				conn = &diagnosticEndpoint{owned}
			}
			commands := 0
			rpc := &transport{conn: conn}
			if tc.stop {
				rpc.stop = func() { commands++ }
			}
			if tc.disconnected {
				rpc.terminal = ErrClosed
			}
			s := NewSession(SessionOptions{})
			s.client, s.closed, s.closing = &Client{rpc: rpc}, tc.closed, tc.closing
			status := s.DiagnosticStatus()
			if s.OwnsLiveRuntime() != tc.wantOwned || status["ownsLiveRuntime"] != tc.wantOwned || status["fenceable"] != (tc.wantOwned && OwnedRuntimeSupported()) || status["terminalEndpointAvailable"] != tc.wantTerminal {
				t.Fatal("ownership and terminal capabilities were conflated", status)
			}
			encoded, err := json.Marshal(status)
			if err != nil || strings.Contains(string(encoded), "PRIVATE_ENDPOINT_SENTINEL") {
				t.Fatal("native endpoint escaped boolean diagnostic projection")
			}
			if commands != 0 || base.commands != 0 {
				t.Fatal("read-only ownership projection dispatched native commands")
			}
		})
	}
	s := NewSession(SessionOptions{})
	status := s.DiagnosticStatus()
	if status["ownsLiveRuntime"] != false || status["fenceable"] != false || status["terminalEndpointAvailable"] != false {
		t.Fatal("absent connection advertised capabilities", status)
	}
}
