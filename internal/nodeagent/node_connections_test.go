//go:build (darwin && cgo) || linux

package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// Actual isolated public setup foreground. Its only mutation is the synthetic
// native connection endpoint. Request bodies and user values are never logged.
func TestNodeSetupProcessHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return
	}
	args := os.Args[sep+1:]
	if len(args) != 5 || args[0] != "serve" || args[1] != "--store-dir" || args[3] != "--listen" {
		os.Exit(2)
	}
	store := args[2]
	listener, err := net.Listen("tcp", args[4])
	if err != nil {
		os.Exit(3)
	}
	tool := exec.Command("/bin/sleep", "120")
	if tool.Start() != nil {
		os.Exit(4)
	}
	if os.MkdirAll(filepath.Join(store, "runtime/service"), 0700) != nil {
		os.Exit(5)
	}
	discovery, _ := json.Marshal(map[string]any{"schema_version": "caelis.control.service-discovery/v1", "endpoint": "http://" + listener.Addr().String(), "instance_id": "owned-node-setup", "principal_id": "synthetic-target-owner"})
	if os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), discovery, 0600) != nil {
		os.Exit(6)
	}
	_ = os.WriteFile(filepath.Join(store, "runtime/service/auth.token"), []byte("SYNTHETIC_NODE_SDK_TOKEN"), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-pids"), []byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(tool.Process.Pid)), 0600)
	caps := []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1", "model-auth-stream-v1"}
	var mu sync.Mutex
	connected := false
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		requests, _ := os.OpenFile(filepath.Join(store, "fixture-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if requests != nil {
			_, _ = requests.WriteString(r.Method + " " + path + "\n")
			_ = requests.Close()
		}
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_NODE_SDK_TOKEN" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		emit := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch path {
		case "/initialize":
			emit(map[string]any{"protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1", "store_id": "native-test-store", "instance_id": "owned-node-setup", "capabilities": caps})
		case "/status":
			emit(map[string]any{"configuration": map[string]string{"revision": "7"}})
		case "/agents/binding-status":
			emit(map[string]any{"targets": []any{}, "handles": []any{}})
		case "/agents/disconnect-candidates":
			emit(map[string]any{"revision": "7", "providers": []any{}, "agents": []any{}})
		case "/completion/slash-arguments":
			var req wire.CompletionRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.Command == nil {
				w.WriteHeader(400)
				return
			}
			command := *req.Command
			var choices []map[string]any
			switch {
			case command == "model":
				if connected {
					choices = []map[string]any{{"value": "target-model", "display": "Target Model"}}
				}
			case command == "connect-provider-api-key" || command == "connect-provider-account":
				choices = []map[string]any{{"value": "target-provider", "display": "Target Provider"}}
			case strings.HasPrefix(command, "connect-baseurl:"):
				choices = []map[string]any{{"value": "https://example.invalid/v1"}}
			case strings.HasPrefix(command, "connect-model:"):
				choices = []map[string]any{{"value": "target-model"}}
			}
			if choices == nil {
				choices = []map[string]any{}
			}
			emit(choices)
		case "/configuration/connect-model":
			var input wire.ConnectModelRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.OperationId == nil {
				w.WriteHeader(400)
				return
			}
			if input.Config.ApiKey == nil || *input.Config.ApiKey != "SYNTHETIC_MANUAL_KEY" {
				w.WriteHeader(400)
				return
			}
			if input.Config.Model == "error-model" {
				emit(map[string]string{"operation_id": *input.OperationId, "outcome": "rejected", "detail": "SYNTHETIC_MANUAL_KEY PRIVATE_ERROR_ECHO"})
				return
			}
			connected = true
			emit(map[string]string{"operation_id": *input.OperationId, "outcome": "committed", "revision": "7"})
		default:
			w.WriteHeader(404)
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

func realNodeConnectionFixture(t *testing.T) (*Service, string) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	watchdogBinary := filepath.Join(directory, "caelis-public-test")
	build := exec.CommandContext(t.Context(), "go", "test", "-c", "-o", watchdogBinary, "./internal/backend/caelis")
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	build.Dir = filepath.Clean(filepath.Join(packageDir, "..", ".."))
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal("watchdog fixture build", err, string(output))
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(directory, "caelis")
	helper := filepath.Join(directory, "caelis-node")
	body := "#!/bin/sh\nif [ \"$1\" = version ]; then printf '{\"version\":\"0.65.0\"}'; exit 0; fi\nexec " + quote(executable) + " -test.run='^TestNodeSetupProcessHelper$' -- \"$@\"\n"
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(watchdogBinary)+" -test.run='^TestOwnedCaelisWatchdogHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	service, err := New(Options{Directory: directory, NodeID: "node-test", Binaries: map[api.NodeBackend]string{api.NodeCodex: "/absent-codex", api.NodeCaelis: binary}})
	if err != nil {
		t.Fatal(err)
	}
	return service, filepath.Join(directory, "caelis-store")
}
func framedConnectionClient(t *testing.T, s *Service) (*Client, func()) {
	return framedConnectionHandler(t, s, Handler(s))
}
func framedConnectionHandler(t *testing.T, s *Service, handler http.Handler) (*Client, func()) {
	t.Helper()
	local, remote := net.Pipe()
	life, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- productrpc.ServeNativeStream(life, remote, remote, handler, allowed) }()
	client, err := NewClient(s.options.NodeID, local)
	if err != nil {
		t.Fatal(err)
	}
	return client, func() { _ = client.Close(); cancel(); _ = local.Close(); _ = remote.Close(); <-done }
}
func waitNodeConnection(t *testing.T, c *Client, ref api.NodeRuntimeConnectionRef, flow api.RuntimeFlow) api.RuntimeFlow {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for flow.Stage != "complete" && flow.Stage != "failed" && flow.Stage != "unknown" {
		var err error
		flow, err = c.WaitNodeRuntimeConnection(ctx, ref, flow.ID, flow.Sequence)
		if err != nil {
			t.Fatal(err)
		}
	}
	return flow
}
func TestNodeConnectionsColdExplicitBeginRealFramedSDKAndConfirmedClose(t *testing.T) {
	s, store := realNodeConnectionFixture(t)
	var dropped atomic.Bool
	base := Handler(s)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == connectionPrefix+"start" && !dropped.Swap(true) {
			recorder := httptest.NewRecorder()
			base.ServeHTTP(recorder, r)
			w.WriteHeader(recorder.Code) // intentionally lose response only
			return
		}
		base.ServeHTTP(w, r)
	})
	c, detach := framedConnectionHandler(t, s, handler)
	defer detach()
	view, err := c.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || view.Guard.Revision == "" || view.ConfigurationAvailable {
		t.Fatal("cold guard unavailable", view, err)
	}
	second, err := c.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || second.Guard != view.Guard {
		t.Fatal("cold guard unstable", second, err)
	}
	if _, err := os.Lstat(store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passive read prepared Store", err)
	}
	stale := view.Guard
	stale.Revision = "stale"
	if _, err := c.BeginNodeRuntimeConnection(t.Context(), stale, "stale-begin"); err == nil {
		t.Fatal("stale Begin admitted")
	}
	if _, err := os.Lstat(store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale Begin prepared Store", err)
	}
	ref, err := c.BeginNodeRuntimeConnection(t.Context(), view.Guard, "user-original-begin")
	if err != nil {
		t.Fatal("explicit cold Begin", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = s.CloseNodeRuntimeConnection(ctx, ref)
	})
	record, err := s.connectionRecord(ref)
	if err != nil || record.Outcome != "active" || record.CleanupConfirmed {
		t.Fatal("original intent absent", record, err)
	}
	live, err := c.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || !live.ConfigurationAvailable || live.Guard == view.Guard || len(live.Configuration.Models) != 0 {
		t.Fatal("cold/live guard omitted native revision", live, err)
	}
	before, err := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	// Host startup changed the public configuration, but lost Begin response
	// recovery returns only the original owner without a second launch.
	replay, err := c.BeginNodeRuntimeConnection(t.Context(), view.Guard, ref.OperationID)
	if err != nil || replay != ref {
		t.Fatal("original Begin recovery failed", replay, err)
	}
	after, _ := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if !bytes.Equal(before, after) {
		t.Fatal("original Begin relaunched")
	}
	catalog, err := c.NodeRuntimeConnectionCatalog(t.Context(), ref, "api-key")
	if err != nil || len(catalog.Choices) != 1 || catalog.Choices[0].ID != "target-provider" {
		t.Fatal(catalog, err)
	}
	choices, err := c.NodeRuntimeSetupCatalog(t.Context(), ref, "models", "target-provider", "https://example.invalid/v1")
	if err != nil || len(choices) != 1 || choices[0].Value != "target-model" {
		t.Fatal(choices, err)
	}
	foreign := ref
	foreign.NodeID = "foreign"
	if _, err := c.NodeRuntimeConnectionCatalog(t.Context(), foreign, "api-key"); err == nil {
		t.Fatal("foreign target accepted")
	}
	override := api.RuntimeSettings{Runtime: "caelis", CaelisStore: "/arbitrary"}
	if _, err := c.StartNodeRuntimeConnection(t.Context(), ref, api.RuntimeConnectionInput{Settings: &override, Kind: "api-key"}); err == nil {
		t.Fatal("renderer settings accepted")
	}
	input := api.RuntimeConnectionInput{Kind: "api-key", Choice: "target-provider", Model: "target-model", APIKey: "SYNTHETIC_MANUAL_KEY"}
	if _, err := c.StartNodeRuntimeConnection(t.Context(), ref, input); err == nil {
		t.Fatal("lost Start response called successful")
	} else {
		var failure *NodeConnectionError
		if !errors.As(err, &failure) || !failure.Unknown {
			t.Fatal("lost Start response discarded uncertainty", err)
		}
	}
	flow, err := c.StartNodeRuntimeConnection(t.Context(), ref, input)
	if err != nil {
		t.Fatal(err)
	}
	flow = waitNodeConnection(t, c, ref, flow)
	if flow.Stage != "complete" {
		t.Fatal("native connection not completed", flow)
	}
	if _, err := c.AdvanceNodeRuntimeConnection(t.Context(), ref, api.RuntimeFlowAction{ID: flow.ID, Action: "refresh"}); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelNodeRuntimeConnection(t.Context(), ref, flow.ID); err != nil {
		t.Fatal(err)
	}
	badRef := ref
	badRef.OperationID = "different-interaction"
	if _, err := c.WaitNodeRuntimeConnection(t.Context(), badRef, flow.ID, 0); err == nil {
		t.Fatal("flow widened operation scope")
	}
	// Repeated Start after a lost response returns the same native flow.
	recovered, err := c.StartNodeRuntimeConnection(t.Context(), ref, api.RuntimeConnectionInput{Kind: "api-key", Choice: "target-provider", Model: "target-model", APIKey: "SYNTHETIC_MANUAL_KEY"})
	if err != nil || recovered.ID != flow.ID || recovered.Stage != "complete" {
		t.Fatal("lost Start response created replacement", recovered, err)
	}

	// Observer disconnect does not own the native setup lease.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	pidFields := strings.Fields(string(before))
	if len(pidFields) != 2 {
		t.Fatal("foreground identities absent")
	}
	for _, p := range pidFields {
		pid, _ := strconv.Atoi(p)
		if syscall.Kill(pid, 0) != nil {
			t.Fatal("observer detached owner stopped")
		}
	}
	if err := s.CloseNodeRuntimeConnection(t.Context(), ref); err != nil {
		t.Fatal("Close cleanup unconfirmed", err)
	}
	for _, p := range pidFields {
		pid, _ := strconv.Atoi(p)
		if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			t.Fatal("Close did not stop exact root/tool")
		}
	}
	if _, err := os.Lstat(filepath.Join(store, "runtime/service/discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Close left discovery", err)
	}
	if err := s.CloseNodeRuntimeConnection(t.Context(), ref); err != nil {
		t.Fatal("original Close not reconciled", err)
	}
	s.connectionsMu.Lock()
	retained := s.connections[ref.OperationID] != nil
	s.connectionsMu.Unlock()
	if retained {
		t.Fatal("closed setup retained transient SDK interaction")
	}

	journal, err := os.ReadFile(s.connectionPath(ref))
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SYNTHETIC_MANUAL_KEY", "SYNTHETIC_NODE_SDK_TOKEN", store, flow.ID, "PRIVATE_ERROR_ECHO"} {
		if bytes.Contains(journal, []byte(private)) {
			t.Fatal("journal persisted private SDK data")
		}
	}
	requests, err := os.ReadFile(filepath.Join(store, "fixture-requests"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(requests), "POST /configuration/connect-model\n") != 1 {
		t.Fatal("lost/repeated Start replayed native credential mutation")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(requests)), "\n") {
		switch line {
		case "GET /initialize", "GET /status", "POST /agents/binding-status", "POST /agents/disconnect-candidates", "POST /completion/slash-arguments", "POST /configuration/connect-model":
		default:
			t.Fatal("unexpected execution request", line)
		}
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.BeginNodeRuntimeConnection(t.Context(), view.Guard, ref.OperationID); err == nil {
		t.Fatal("closed original Begin started replacement")
	}
}

type syntheticNodeConnectionOwner struct {
	settings api.RuntimeSettings
	done     chan struct{}
	once     sync.Once
	closeErr error
}

func (o *syntheticNodeConnectionOwner) SetupSettings(ctx context.Context) (api.RuntimeSettings, error) {
	return o.settings, ctx.Err()
}
func (o *syntheticNodeConnectionOwner) Check(ctx context.Context) error { return ctx.Err() }
func (o *syntheticNodeConnectionOwner) Close(context.Context) error {
	o.once.Do(func() { close(o.done) })
	return o.closeErr
}
func (o *syntheticNodeConnectionOwner) Done() <-chan struct{} { return o.done }
func TestNodeConnectionsUnknownBeginOrCloseNeverReplaysOrStartsNewID(t *testing.T) {
	for _, phase := range []string{"begin", "close"} {
		t.Run(phase, func(t *testing.T) {
			s, r, _ := readinessFixture(t)
			s.options.Configurations[api.NodeCaelis] = nil
			calls := new(atomic.Int32)
			s.beginSetup = func(ctx context.Context, o caelis.OwnedHostOptions) (NodeConnectionOwner, error) {
				calls.Add(1)
				var original nodeConnectionRecord
				ref := api.NodeRuntimeConnectionRef{NodeID: r.NodeID, Backend: r.Backend, OperationID: "original-user-begin"}
				if readPrivateJSON(s.connectionPath(ref), &original) != nil || original.Outcome != "intent" {
					t.Error("Host preceded intent")
				}
				if phase == "begin" {
					return nil, errors.New("SYNTHETIC_PRIVATE_CAUSE")
				}
				return &syntheticNodeConnectionOwner{settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: o.Binary, CaelisStore: o.Store}, done: make(chan struct{}), closeErr: errors.New("SYNTHETIC_PRIVATE_STOP")}, nil
			}
			view, err := s.Configuration(t.Context(), r.NodeID, r.Backend)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, "original-user-begin")
			if phase == "begin" && err == nil {
				t.Fatal("unknown begin claimed active")
			}
			if phase == "close" {
				if err != nil {
					t.Fatal(err)
				}
				if err := s.CloseNodeRuntimeConnection(t.Context(), ref); err == nil {
					t.Fatal("unknown cleanup called confirmed")
				}
			}
			if _, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, ref.OperationID); err == nil {
				t.Fatal("original unknown replayed")
			}
			if _, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, "new-user-begin"); err == nil {
				t.Fatal("new ID bypassed unknown")
			}
			restarted, err := New(s.options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.BeginNodeRuntimeConnection(t.Context(), view.Guard, "after-restart-begin"); err == nil {
				t.Fatal("restart bypassed unknown")
			}
			if calls.Load() != 1 {
				t.Fatal("Host redispatched", calls.Load())
			}
			if phase == "begin" && err != nil && strings.Contains(err.Error(), "SYNTHETIC_PRIVATE") {
				t.Fatal("private error exposed")
			}
		})
	}
}
func TestNodeConnectionsClosedInputsAndVerifiedHelperBeforeStorePreparation(t *testing.T) {
	s, store := realNodeConnectionFixture(t)
	view, err := s.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil {
		t.Fatal(err)
	}
	helperBody, err := os.ReadFile(filepath.Join(s.options.Directory, "caelis-node"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.options.Directory, "caelis-node")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, "no-helper-begin"); err == nil {
		t.Fatal("missing verified helper admitted")
	}
	if _, err := os.Lstat(store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing helper wrote Store")
	}
	input, _ := json.Marshal(nodeConnectionBeginRequest{Guard: view.Guard, OperationID: "unknown-input"})
	input = append(input[:len(input)-1], []byte(`,"store":"/arbitrary"}`)...)
	reply := httptest.NewRecorder()
	Handler(s).ServeHTTP(reply, httptest.NewRequest(http.MethodPost, connectionPrefix+"begin", bytes.NewReader(input)))
	if reply.Code != 400 {
		t.Fatal("unknown authority accepted", reply.Code)
	}
	if allowed("POST", connectionPrefix+"arbitrary") || allowed("GET", connectionPrefix+"begin") {
		t.Fatal("open endpoint union")
	}
	if err := os.Mkdir(store, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.options.Directory, "caelis-node"), helperBody, 0700); err != nil {
		t.Fatal(err)
	}
	// A preexisting unmarked Store is never adopted even with the helper restored.
	if _, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, "unmarked-begin"); err == nil {
		t.Fatal("unmarked Store admitted")
	}
	if _, err := os.Lstat(filepath.Join(store, ".caelis-bot-node-owner.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unmarked Store adopted")
	}
}

func TestNodeConnectionsSDKFailureEchoIsSafeAndStartNeverRedispatches(t *testing.T) {
	s, r, _ := readinessFixture(t)
	s.options.Configurations[api.NodeCaelis] = nil
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, "/api/control/v1")
		w.Header().Set("Content-Type", "application/json")
		emit := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		caps := []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1"}
		switch path {
		case "/initialize":
			emit(map[string]any{"protocol_version": 1, "api_version": "v1", "envelope_version": "caelis.control.envelope/v1", "store_id": "native-store", "instance_id": "synthetic-borrowed-owned-host", "capabilities": caps})
		case "/status":
			emit(map[string]any{"configuration": map[string]string{"revision": "7"}})
		case "/completion/slash-arguments":
			emit([]map[string]string{{"value": "target-provider"}})
		case "/configuration/connect-model":
			var input wire.ConnectModelRequest
			if json.NewDecoder(request.Body).Decode(&input) != nil || input.OperationId == nil {
				w.WriteHeader(400)
				return
			}
			emit(map[string]string{"operation_id": *input.OperationId, "outcome": "rejected", "detail": "SYNTHETIC_MANUAL_KEY PRIVATE_ERROR_ECHO"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	serviceDir := filepath.Join(r.ExpectedStore, "runtime/service")
	if err := os.MkdirAll(serviceDir, 0700); err != nil {
		t.Fatal(err)
	}
	discovery, _ := json.Marshal(map[string]string{"schema_version": "caelis.control.service-discovery/v1", "endpoint": server.URL, "instance_id": "synthetic-borrowed-owned-host", "principal_id": "synthetic-owner"})
	if err := os.WriteFile(filepath.Join(serviceDir, "discovery.json"), discovery, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "auth.token"), []byte("SYNTHETIC_BORROWED_TOKEN"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := &syntheticNodeConnectionOwner{settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: r.ExpectedBinary, CaelisStore: r.ExpectedStore}, done: make(chan struct{})}
	s.options.NodeConnectionOwner = func(context.Context, OwnedRuntimeSettings) (NodeConnectionOwner, error) { return owner, nil }
	view, err := s.Configuration(t.Context(), r.NodeID, r.Backend)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.BeginNodeRuntimeConnection(t.Context(), view.Guard, "borrowed-original-begin")
	if err != nil {
		t.Fatal(err)
	}
	c, detach := framedConnectionClient(t, s)
	defer detach()
	input := api.RuntimeConnectionInput{Kind: "api-key", Choice: "target-provider", Model: "target-model", APIKey: "SYNTHETIC_MANUAL_KEY"}
	flow, err := c.StartNodeRuntimeConnection(t.Context(), ref, input)
	if err != nil {
		t.Fatal(err)
	}
	flow = waitNodeConnection(t, c, ref, flow)
	if flow.Stage != "failed" || flow.Message != "node-connection-failed" {
		t.Fatal("private native rejection not sanitized", flow)
	}
	body, _ := json.Marshal(flow)
	if bytes.Contains(body, []byte("SYNTHETIC_MANUAL_KEY")) || bytes.Contains(body, []byte("PRIVATE_ERROR_ECHO")) {
		t.Fatal("SDK echoed user input")
	}
	replay, err := c.StartNodeRuntimeConnection(t.Context(), ref, input)
	if err != nil || replay.ID != flow.ID {
		t.Fatal("unknown Start replaced original flow", replay, err)
	}
	if err := c.CloseNodeRuntimeConnection(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	// Borrowed setup Close detaches only; the separate native fixture stays live.
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal("borrowed Close stopped warm Host", err)
	}
	_ = response.Body.Close()
	journal, err := os.ReadFile(s.connectionPath(ref))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte("SYNTHETIC")) || bytes.Contains(journal, []byte(flow.ID)) {
		t.Fatal("journal retained SDK data")
	}
	cause := errors.New("SYNTHETIC_PRIVATE_ERROR")
	safe := connectionSDKError("advance outcome unavailable", cause)
	if !errors.Is(safe, cause) || strings.Contains(safe.Error(), "SYNTHETIC") {
		t.Fatal("safe error lost classification or leaked cause")
	}
}

// Opt-in acceptance uses only a new empty private Store and the explicitly
// supplied installed release/helper. It never enters a user Store or supplies
// account credentials, calls a model, or installs/deploys anything.
func TestNodeConnectionsInstalledPublicCaelisEmptyStore(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_BINARY")
	helper := os.Getenv("CAELIS_BOT_TEST_HOST_BINARY")
	if binary == "" || helper == "" {
		t.Skip("requires explicit installed Caelis and verified native companion")
	}
	if !cleanOwnedRuntimePath(binary) || !cleanOwnedRuntimePath(helper) {
		t.Fatal("absolute native test executable paths required")
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	s, err := New(Options{Directory: directory, NodeID: "native-empty-caelis-test", Binaries: map[api.NodeBackend]string{api.NodeCodex: "/absent-codex", api.NodeCaelis: binary}, OwnedRuntimeCompanion: func(context.Context) (OwnedRuntimeCompanion, error) {
		return OwnedRuntimeCompanion{Path: helper, SHA256: hex.EncodeToString(hash[:])}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	c, detach := framedConnectionClient(t, s)
	defer detach()
	view, err := c.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || view.Guard.Revision == "" || view.ConfigurationAvailable {
		t.Fatal("cold installed runtime guard", err)
	}
	store := filepath.Join(directory, "caelis-store")
	if _, err := os.Lstat(store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passive read changed Store")
	}
	ref, err := c.BeginNodeRuntimeConnection(t.Context(), view.Guard, "explicit-empty-setup")
	if err != nil {
		t.Fatal("real installed empty setup Begin", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = s.CloseNodeRuntimeConnection(ctx, ref)
	})
	read, err := c.Configuration(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || !read.ConfigurationAvailable || len(read.Configuration.Models) != 0 {
		t.Fatal("real empty public configuration", err)
	}
	if _, err := c.NodeRuntimeConnectionCatalog(t.Context(), ref, "api-key"); err != nil {
		t.Fatal("real public provider catalog", err)
	}
	if _, err := c.NodeRuntimeSetupCatalog(t.Context(), ref, "providers", "", ""); err != nil {
		t.Fatal("real public setup catalog", err)
	}
	models, err := c.NodeRuntimeSetupCatalog(t.Context(), ref, "models", "", "")
	if err == nil && len(models) != 0 {
		t.Fatal("empty Store acquired models")
	}
	if err := c.CloseNodeRuntimeConnection(t.Context(), ref); err != nil {
		t.Fatal("real setup Close", err)
	}
	if eligible, reason := caelis.ProbeOwnedStore(s.options.NodeID, store); !eligible {
		t.Fatal("real setup remained live", reason)
	}
}
