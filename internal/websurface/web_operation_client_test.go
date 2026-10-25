package websurface

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type rejectedConnectionFixture struct {
	api.MachineController
	calls atomic.Int32
}

func (f *rejectedConnectionFixture) ConnectBot(_ context.Context, in api.BotConnectionInput) (api.Machine, error) {
	f.calls.Add(1)
	if in.Code == "uncertain-fixture" {
		return api.Machine{}, errors.New("private transport or decoded-response failure")
	}
	if in.Code != "correct-fixture" && in.Code != "lost-fixture" {
		return api.Machine{}, &api.ActionRejectedError{Code: "pairing_code_invalid"}
	}
	return api.Machine{ID: "original-fixture-peer", Name: in.Name, Address: in.Address, State: "ready"}, nil
}

type rejectedEndpointFixture struct {
	api.WorkerSharingController
	calls     atomic.Int32
	pairCalls atomic.Int32
}

func (f *rejectedEndpointFixture) BeginWorkerPairing(context.Context) (api.WorkerSharing, error) {
	if f.pairCalls.Add(1) == 1 {
		return api.WorkerSharing{}, &api.ActionRejectedError{Code: "worker_not_ready"}
	}
	return api.WorkerSharing{Enabled: true, Address: "http://127.0.0.1:29317", Code: "123456", ExpiresAt: 2_000_000_000}, nil
}

func (f *rejectedEndpointFixture) SetWorkerEndpoint(in api.WorkerSharingInput) (api.WorkerSharing, error) {
	f.calls.Add(1)
	if in.Listen == "invalid-fixture" {
		return api.WorkerSharing{}, errors.New("worker_listener_invalid")
	}
	return api.WorkerSharing{Listen: in.Listen, Address: in.Address}, nil
}

func TestWebTransportActualHandlerCorrectsKnownRefusalsAndKeepsUnknownProof(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node 24 is required for the actual TypeScript Web client fixture", err)
	}
	h := httptest.NewUnstartedServer(nil)
	origin := "http://" + h.Listener.Addr().String()
	s := testServerConfig(t, func(c *Config) { c.Origins = []string{origin} })
	connections, endpoint := &rejectedConnectionFixture{}, &rejectedEndpointFixture{}
	s.services.Backend.ConfigureMachines(connections)
	s.services.Backend.ConfigureWorkerSharing(endpoint)
	var dropped atomic.Bool
	h.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/machines/connect-bot" && !dropped.Load() {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var request actionRequest
			_ = json.Unmarshal(body, &request)
			var input api.BotConnectionInput
			if len(request.Args) == 1 {
				_ = json.Unmarshal(request.Args[0], &input)
			}
			if input.Code == "lost-fixture" && dropped.CompareAndSwap(false, true) {
				s.ServeHTTP(httptest.NewRecorder(), r)
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
		}
		s.ServeHTTP(w, r)
	})
	h.Start()
	t.Cleanup(h.Close)
	// This is an ephemeral fixture browser grant, not a user's credentials.
	code, err := os.ReadFile(filepath.Join(s.config.Directory, "pairing-code"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(string(code))})
	r := httptest.NewRequest("POST", origin+"/api/v1/pair", bytes.NewReader(data))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Caelis-Request", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var paired struct{ CSRF string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &paired) != nil || len(w.Result().Cookies()) != 1 {
		t.Fatal("fixture browser authentication failed", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	configuration, _ := json.Marshal(map[string]string{"origin": origin, "cookie": cookie.Name + "=" + cookie.Value, "csrf": paired.CSRF})
	cmd := exec.CommandContext(t.Context(), node, filepath.Join("testdata", "web-operation-client.mjs"))
	cmd.Stdin = bytes.NewReader(configuration)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual Web transport/handler chain failed: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
	if connections.calls.Load() != 4 || endpoint.calls.Load() != 2 || endpoint.pairCalls.Load() != 2 || !dropped.Load() {
		t.Fatal("client replayed known/unknown actions or skipped fixture effects", connections.calls.Load(), endpoint.calls.Load())
	}
	journal, err := os.ReadFile(filepath.Join(s.config.Directory, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"wrong-fixture", "correct-fixture", "lost-fixture", "uncertain-fixture", "private transport"} {
		if bytes.Contains(journal, []byte(private)) {
			t.Fatal("operation journal retained pairing arguments or private owner error")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unknown := 0
	for _, op := range s.operations {
		if op.Error == "operation_unconfirmed" {
			unknown++
			originalArgs := `[{"id":"","name":"Fixture B","address":"http://127.0.0.1:12345","code":"uncertain-fixture"}]`
			if op.Digest != digest("/api/v1/machines/connect-bot"+originalArgs) || op.Owner == "" {
				t.Fatal("uncertain operation lost original proof")
			}
		}
	}
	if unknown != 1 {
		t.Fatal("actual network uncertainty was cleared or duplicated", unknown)
	}
}

func (f *rejectedConnectionFixture) PrepareBotConnection(in api.BotConnectionInput, _ *api.BotConnectionReference) (api.BotConnectionReference, error) {
	return api.BotConnectionReference{ID: "original-fixture-peer", Pairing: "fixture-intent", Proof: "fixture-proof"}, nil
}
func (f *rejectedConnectionFixture) ConnectPreparedBot(ctx context.Context, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	v, e := f.ConnectBot(ctx, in)
	return v, ref, e
}
func (f *rejectedConnectionFixture) BotConnectionQuiescent(api.BotConnectionReference) bool {
	return true
}
func (f *rejectedConnectionFixture) AbortPreparedBotConnection(api.BotConnectionReference) error {
	return nil
}
