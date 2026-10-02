package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type preflightManagementPort struct {
	*configurationPortFixture
	installs *atomic.Int32
}

func (*preflightManagementPort) Capabilities() productmanagement.Capabilities {
	return productmanagement.Capabilities{Installation: true, Configuration: true}
}
func (p *preflightManagementPort) ManageRuntime(_ context.Context, c productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
	p.installs.Add(1)
	return productmanagement.RuntimeResult{Scope: c.Scope, ID: c.ID, Outcome: "accepted", Status: runtimemanagement.Status{Runtime: c.Runtime, RequestID: c.ID, Outcome: "accepted", Installed: true, Version: c.Version}}, nil
}

func TestManagementPreflightAndRejectionThroughActualFramedRPC(t *testing.T) {
	for _, kind := range []string{"configuration", "installation", "execution"} {
		t.Run(kind, func(t *testing.T) {
			port := &thinProductPort{snapshot: api.Snapshot{Connection: "ready", CanSend: true}}
			native := &nativeConfigurationFixture{}
			execution := &executionSourceFixture{state: productmanagement.ExecutionState{Conversation: api.ExecutionSettings{Model: "public/model", Effort: "medium"}}}
			var installs, commands atomic.Int32
			token := strings.Repeat("synthetic-product-only-", 3)
			server, err := productrpc.NewServer(port, productrpc.Options{NodeID: "node-fixture", BotID: "bot-fixture", Token: token, JournalFile: filepath.Join(t.TempDir(), "journal.json"), Management: func(scope productmanagement.Scope) (productmanagement.Port, error) {
				return &preflightManagementPort{&configurationPortFixture{scope: scope, configuration: native}, &installs}, nil
			}, Execution: func(scope productmanagement.Scope) (productmanagement.ExecutionPort, error) {
				return productmanagement.NewExecution(scope, execution)
			}})
			if err != nil {
				t.Fatal(err)
			}
			port.server = server
			var mode atomic.Int32 // 1: server validation rejection; 2: committed response lost.
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/commands" {
					server.ServeHTTP(w, r)
					return
				}
				commands.Add(1)
				if mode.Load() == 1 {
					// Reach the real server's pre-journal rejection boundary after a protocol
					// version mismatch in flight, rather than substituting a fake client error.
					var command productrpc.Command
					if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
						t.Error(err)
					}
					command.Kind = "unsupported-fixture-command"
					body, _ := json.Marshal(command)
					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = int64(len(body))
				}
				if mode.Load() == 2 {
					response := httptest.NewRecorder()
					server.ServeHTTP(response, r)
					if response.Code != 200 {
						t.Errorf("native dispatch did not commit: %d", response.Code)
					}
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return
				}
				server.ServeHTTP(w, r)
			}))
			defer host.Close()
			e, err := newProductEngine(t.TempDir(), thinPairing(), func(backend.ProductPairing) (nativeProductClient, io.Closer, error) {
				local, remote := net.Pipe()
				t.Cleanup(func() { remote.Close() })
				go func() { _ = productrpc.ProxyStdio(t.Context(), remote, remote, host.URL, token) }()
				client, err := productrpc.NewStdioClient(productrpc.StdioOptions{ExpectedNode: "node-fixture", ExpectedBot: "bot-fixture"}, local)
				return client, local, err
			})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close(context.Background())
			if err = e.Connect(t.Context()); err != nil {
				t.Fatal(err)
			}
			state, err := e.RemoteRuntime(t.Context())
			if err != nil || !state.Available {
				t.Fatal(state, err)
			}
			mutate := func(id string, invalid bool) (backend.RemoteManagementResult, error) {
				switch kind {
				case "configuration":
					description := "valid role"
					if invalid {
						description = strings.Repeat("d", 8193)
					}
					return e.ChangeRemoteRuntimeConfiguration(t.Context(), backend.RemoteConfigurationRequest{ID: id, Binding: state.Binding, Change: api.RuntimeConfigurationChange{Action: "create-role", ID: "role", ExpectedRevision: "42", Description: description}})
				case "installation":
					version := "0.65.0"
					if invalid {
						version = strings.Repeat("v", 129)
					}
					return e.ManageRemoteRuntime(t.Context(), backend.RemoteRuntimeRequest{ID: id, Binding: state.Binding, Action: "install", Runtime: "caelis", Version: version})
				default:
					model := "public/model"
					if invalid {
						model = strings.Repeat("m", 257)
					}
					return e.ChangeRemoteExecutionSettings(t.Context(), backend.RemoteExecutionRequest{ID: id, Binding: state.Binding, Target: "conversation", ExpectedRevision: productmanagement.ExecutionRevision(execution.state), Selection: productmanagement.Selection{Model: model, Effort: "high"}})
				}
			}
			if _, err = mutate("invalid", true); err == nil || len(e.receipts.Pending) != 0 || commands.Load() != 0 {
				t.Fatal("invalid input reserved/dispatched", err, e.receipts.Pending, commands.Load())
			}
			mode.Store(1)
			result, err := mutate("server-rejected", false)
			if err == nil || result.Outcome != "rejected" || len(e.receipts.Pending) != 0 || !e.Snapshot().CanSend || commands.Load() != 1 || native.calls+execution.calls+int(installs.Load()) != 0 {
				t.Fatal("proven rejection became unknown", result, err, e.receipts.Pending)
			}
			mode.Store(0)
			if result, err = mutate("valid", false); err != nil || result.Outcome != "accepted" {
				t.Fatal("valid request after rejection failed", result, err)
			}
			mode.Store(2)
			if result, err = mutate("lost-response", false); err == nil || result.Outcome != "unknown" || len(e.receipts.Pending) != 1 || e.Snapshot().CanSend {
				t.Fatal("lost response did not retain original intent", result, err)
			}
			mode.Store(0)
			if err = e.Connect(t.Context()); err != nil {
				t.Fatal(err)
			}
			state, err = e.RemoteRuntime(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(e.receipts.Pending) != 0 || native.calls+execution.calls+int(installs.Load()) != 2 {
				t.Fatal("original receipt did not reconcile exactly once", e.receipts.Pending)
			}
		})
	}
}
