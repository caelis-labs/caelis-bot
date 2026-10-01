//go:build (darwin && cgo) || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

func roamingTestPointer[T any](v T) *T { return &v }

// A public control-protocol foreground fixture with synthetic authentication.
func TestRoamingCaelisWorkerNativeHelper(t *testing.T) {
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
	listener, e := net.Listen("tcp", args[4])
	if e != nil {
		os.Exit(3)
	}
	if os.MkdirAll(filepath.Join(store, "runtime/service"), 0700) != nil {
		os.Exit(4)
	}
	discovery, _ := json.Marshal(map[string]string{"schema_version": "caelis.control.service-discovery/v1", "endpoint": "http://" + listener.Addr().String(), "instance_id": "fixture-instance", "principal_id": "fixture-owner"})
	if os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), discovery, 0600) != nil {
		os.Exit(5)
	}
	if os.WriteFile(filepath.Join(store, "runtime/service/auth.token"), []byte("SYNTHETIC_CONTAINED_TOKEN"), 0600) != nil {
		os.Exit(6)
	}
	_ = os.WriteFile(filepath.Join(store, "fixture-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	var mu sync.Mutex
	var secret string
	var profile wire.ApplicationProfile
	life := func() wire.ApplicationConnection {
		return wire.ApplicationConnection{ApplicationId: "worker-app", ConnectionId: "worker-connection", PrincipalId: "fixture-owner", ExpiresAt: time.Now().Add(time.Hour)}
	}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		mu.Lock()
		allowed := r.Header.Get("Authorization") == "Bearer SYNTHETIC_CONTAINED_TOKEN" || secret != "" && r.Header.Get("Authorization") == "Bearer "+secret
		mu.Unlock()
		if !allowed {
			w.WriteHeader(401)
			return
		}
		if path == "/sessions/fixture-worker-session/reconnect" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch path {
		case "/initialize":
			write(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: roamingTestPointer("fixture-store"), InstanceId: roamingTestPointer("fixture-instance"), Capabilities: []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-hot-configuration-v1", "application-native-execution-v1", "application-workspace-binding-v1", "application-background-activation-v1", "application-resource-transfer-v1", "execution-configuration-v1", "application-guardian-review-v1"}})
		case "/status":
			write(w, wire.StatusSnapshot{})
		case "/completion/slash-arguments":
			write(w, []wire.SlashArgCandidate{{Value: "fixture-model", NoAuth: roamingTestPointer(false), ModelSelection: &wire.ModelSelection{Current: roamingTestPointer(true)}}})
		case "/applications/register":
			var req wire.ApplicationRegistration
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				w.WriteHeader(400)
				return
			}
			secret = req.Credential
			_ = os.WriteFile(filepath.Join(store, "worker-enrolled"), nil, 0600)
			write(w, life())
		case "/application/connection":
			write(w, life())
		case "/application/sessions":
			var req wire.CreateApplicationSessionRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				w.WriteHeader(400)
				return
			}
			profile = req.Profile
			write(w, wire.CommandResult{OperationId: *req.OperationId, Outcome: "committed", SessionId: roamingTestPointer("fixture-worker-session")})
		case "/application/sessions/fixture-worker-session":
			write(w, wire.ApplicationBinding{ApplicationId: life().ApplicationId, ConnectionId: life().ConnectionId, PrincipalId: life().PrincipalId, SessionId: "fixture-worker-session", Profile: profile, CreationDigest: "fixture-creation"})
		case "/application/sessions/fixture-worker-session/background-grants":
			var req wire.ApplicationBackgroundGrantRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				w.WriteHeader(400)
				return
			}
			write(w, wire.ApplicationBackgroundGrant{Id: "fixture-grant", ApplicationId: life().ApplicationId, ConnectionId: life().ConnectionId, PrincipalId: life().PrincipalId, SessionId: "fixture-worker-session", Source: req.Source, AuthorizationOperationId: req.AuthorizationOperationId})
		case "/application/sessions/fixture-worker-session/prompt":
			var req wire.ApplicationPromptRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.SourceKind != "authorized_background" || req.GrantId == nil || *req.GrantId != "fixture-grant" {
				w.WriteHeader(400)
				return
			}
			_ = os.WriteFile(filepath.Join(store, "worker-prompted"), nil, 0600)
			write(w, wire.CommandResult{OperationId: *req.OperationId, Outcome: "committed", SessionId: roamingTestPointer("fixture-worker-session")})
		case "/sessions/fixture-worker-session/state":
			write(w, wire.SessionState{SessionId: "fixture-worker-session", Run: wire.RunState{Status: roamingTestPointer("completed")}})
		default:
			_ = os.WriteFile(filepath.Join(store, "unexpected-call"), []byte(path), 0600)
			w.WriteHeader(404)
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}
func TestRoamingCodexPrimaryUsesIndependentApprovedSameNodeCaelisWorker(t *testing.T) {
	root := canonicalWorkerTestRoot(t)
	helper, e := verifiedRoamingExecutable()
	if e != nil {
		t.Fatal(e)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(root, "caelis")
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(helper)+" -test.run='^TestRoamingCaelisWorkerNativeHelper$' -- \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	directory := filepath.Join(root, "agent")
	if e = os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	store := filepath.Join(root, "caelis-store")
	c := roamingCommand{NodeID: "same-node", BotID: "actual-native-bot", Backend: "codex", AgentDirectory: directory, BrokerNodeID: "paired-broker"}
	plan := roamingWorkerPlan{Sources: []roamingWorkerSource{{NodeID: c.NodeID, Backends: []string{"codex"}}}, Runtimes: []roamingWorkerRuntime{{Backend: "caelis", Binary: binary, Store: store, Model: "fixture-model", Execution: &api.WorkExecutionSettings{Model: "fixture-model"}}}}
	reader := &roamingEpochFixture{lease: nodeplane.Lease{BotID: c.BotID, NodeID: c.NodeID, Backend: api.NodeCodex, Epoch: "epoch-1", TTLMs: 60000}}
	workers := newRoamingOwnedWorkers(t.Context(), c, plan, helper, reader, func(context.Context, func(), func()) (func(), error) { return func() {}, nil })
	defer func() {
		if e := workers.Close(); e != nil {
			t.Error(e)
		}
	}()
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: c.NodeID, Backend: "caelis", Role: api.RoleWorker}, BotID: api.ProfileBotID(c.BotID), SourceNode: c.NodeID, SourceBackend: "codex"}
	bad := pair
	bad.Target.Backend = "codex"
	if _, e = workers.resolve(t.Context(), bad); e == nil {
		t.Fatal("undeclared target backend fell back to main backend")
	}
	endpoint, e := workers.resolve(workerTestBound(t), pair)
	if e != nil {
		t.Fatal(e)
	}
	stream, e := net.Dial("unix", endpoint.Socket)
	if e != nil {
		t.Fatal(e)
	}
	worker, e := workerwire.NewClient(workerTestBound(t), pair, reader, stream)
	if e != nil {
		t.Fatal(e)
	}
	defer worker.Close()
	if !worker.LeaseAwareAdmission() {
		t.Fatal("independent alternate backend has no actual owned fence")
	}
	if _, e = os.Stat(filepath.Join(store, "worker-enrolled")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("read-only handshake enrolled application without native activation")
	}
	id := "task-" + strings.Repeat("a", 32)
	workspace, e := worker.ResolveWorkWorkspace(workerTestBound(t), id, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = worker.PrepareWorkWorkspace(workerTestBound(t), id, workspace, false); e != nil {
		unexpected, _ := os.ReadFile(filepath.Join(store, "unexpected-call"))
		t.Fatalf("prepare %v unexpected=%s", e, unexpected)
	}
	source, _ := reader.WorkDispatchSource(t.Context())
	in := api.WorkStart{TaskStart: api.TaskStart{RequestID: "actual-caelis-start", Title: "Contained alternate Worker", Prompt: "Synthetic native effect", Target: &pair.Target}, ID: id, Workspace: workspace, Instructions: "Bounded native Worker policy", Source: source, RequestDigest: strings.Repeat("a", 64)}
	if task, e := worker.StartWork(workerTestBound(t), in); e != nil {
		unexpected, _ := os.ReadFile(filepath.Join(store, "unexpected-call"))
		t.Fatalf("alternate native start %+v %v unexpected=%s", task, e, unexpected)
	}
	if _, e = os.Stat(filepath.Join(store, "worker-prompted")); e != nil {
		t.Fatal("actual native alternate Worker effect missing", e)
	}
	pidbytes, _ := os.ReadFile(filepath.Join(store, "fixture-pid"))
	pid, _ := strconv.Atoi(string(pidbytes))
	worker.Close()
	if e = syscall.Kill(pid, 0); e != nil {
		t.Fatal("observer detach killed alternate owned Worker", e)
	}
	if workers.CaelisStoreInUse() {
		t.Fatal("Codex primary was treated as a Caelis store owner")
	}
}
