//go:build darwin || linux

package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOwnedCaelisWatchdogHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "owned-runtime-watchdog" {
			if err := codex.RunSupervisedRuntime(context.Background(), os.NewFile(3, "owned-watchdog")); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
}

// Public foreground protocol/process fixture; no Core credentials or model.
func TestOwnedCaelisProcessHelper(t *testing.T) {
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
	l, err := net.Listen("tcp", args[4])
	if err != nil {
		os.Exit(3)
	}
	child := exec.Command("/bin/sleep", "60")
	if child.Start() != nil {
		os.Exit(4)
	}
	if os.MkdirAll(filepath.Join(store, "runtime/service"), 0700) != nil {
		os.Exit(5)
	}
	b, _ := json.Marshal(discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: "http://" + l.Addr().String(), InstanceID: "owned-core-fixture", PrincipalID: "fixture-owner"})
	if os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), b, 0600) != nil {
		os.Exit(6)
	}
	_ = os.WriteFile(filepath.Join(store, "runtime/service/auth.token"), []byte("SYNTHETIC_PRIVATE_TOKEN"), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-pids"), []byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(child.Process.Pid)), 0600)
	var fixtureMu sync.Mutex
	var applicationSecret string
	var workerProfile wire.ApplicationProfile
	life := func() wire.ApplicationConnection {
		return wire.ApplicationConnection{ApplicationId: "worker-app", ConnectionId: "worker-connection", PrincipalId: "fixture-owner", ExpiresAt: time.Now().Add(time.Hour)}
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		if path == "/sessions/owned-worker-session/reconnect" {
			fixtureMu.Lock()
			allowed := r.Header.Get("Authorization") == "Bearer SYNTHETIC_PRIVATE_TOKEN" || applicationSecret != "" && r.Header.Get("Authorization") == "Bearer "+applicationSecret
			fixtureMu.Unlock()
			if !allowed {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fixtureMu.Lock()
		defer fixtureMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_PRIVATE_TOKEN" && (applicationSecret == "" || r.Header.Get("Authorization") != "Bearer "+applicationSecret) {
			w.WriteHeader(401)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("owned-store"), InstanceId: pointer("owned-core-fixture"), Capabilities: append(append(append([]string{}, required...), boundedWorkerRequired...), workerRequired...)})
		case "/status":
			writeFixture(w, wire.StatusSnapshot{})
		case "/applications/register":
			var req wire.ApplicationRegistration
			if json.NewDecoder(r.Body).Decode(&req) != nil || r.Header.Get("Authorization") != "Bearer SYNTHETIC_PRIVATE_TOKEN" {
				w.WriteHeader(400)
				return
			}
			applicationSecret = req.Credential
			_ = os.WriteFile(filepath.Join(store, "worker-enrolled"), []byte("enrolled"), 0600)
			writeFixture(w, life())
		case "/sessions/owned-worker-session/state":
			mode, _ := os.ReadFile(filepath.Join(store, "fixture-control-mode"))
			state := wire.SessionState{SessionId: "owned-worker-session", Run: wire.RunState{Status: pointer("completed")}}
			if len(mode) > 0 {
				state.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("t")}
			}
			if strings.Contains(string(mode), "new-turn") {
				state.Run.HandleId = pointer("new-h")
				state.Run.RunId = pointer("new-r")
				state.Run.TurnId = pointer("new-t")
			}
			if strings.Contains(string(mode), "decision") {
				state.Approval.Active = testApproval()
			}
			writeFixture(w, state)
		case "/sessions/owned-worker-session/cancel":
			var req wire.CancelRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || value(req.SessionId) != "owned-worker-session" || req.Target != (wire.TurnTarget{HandleId: "h", RunId: "r", TurnId: "t"}) {
				w.WriteHeader(400)
				return
			}
			recordOwnedControl(store, "cancel", req)
			mode, _ := os.ReadFile(filepath.Join(store, "fixture-control-mode"))
			outcome := wire.Outcome("committed")
			if strings.Contains(string(mode), "unknown") {
				outcome = "unknown"
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: outcome})
		case "/sessions/owned-worker-session/approvals/approval/resolve":
			var req wire.ResolveApprovalRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || value(req.SessionId) != "owned-worker-session" || req.ApprovalRequestId != "approval" || value(req.OptionId) != "native-allow" || req.Target != (wire.TurnTarget{HandleId: "h", RunId: "r", TurnId: "t"}) {
				w.WriteHeader(400)
				return
			}
			recordOwnedControl(store, "decision", req)
			mode, _ := os.ReadFile(filepath.Join(store, "fixture-control-mode"))
			outcome := wire.Outcome("committed")
			if strings.Contains(string(mode), "unknown") {
				outcome = "unknown"
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: outcome})
		case "/application/connection":
			writeFixture(w, life())
		case "/application/sessions":
			var req wire.CreateApplicationSessionRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				w.WriteHeader(400)
				return
			}
			workerProfile = req.Profile
			_ = os.WriteFile(filepath.Join(store, "worker-created"), []byte("created"), 0600)
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("owned-worker-session")})
		case "/application/sessions/owned-worker-session":
			writeFixture(w, wire.ApplicationBinding{ApplicationId: life().ApplicationId, ConnectionId: life().ConnectionId, PrincipalId: life().PrincipalId, SessionId: "owned-worker-session", Profile: workerProfile, CreationDigest: "fixture-creation"})
		case "/application/sessions/owned-worker-session/background-grants":
			var req wire.ApplicationBackgroundGrantRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil {
				w.WriteHeader(400)
				return
			}
			writeFixture(w, wire.ApplicationBackgroundGrant{Id: "owned-grant", PrincipalId: life().PrincipalId, ApplicationId: life().ApplicationId, ConnectionId: life().ConnectionId, SessionId: "owned-worker-session", Source: req.Source, AuthorizationOperationId: req.AuthorizationOperationId})
		case "/application/sessions/owned-worker-session/prompt":
			var req wire.ApplicationPromptRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.SourceKind != "authorized_background" || value(req.GrantId) != "owned-grant" {
				w.WriteHeader(400)
				return
			}
			_ = os.WriteFile(filepath.Join(store, "worker-prompted"), []byte("prompted"), 0600)
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("owned-worker-session")})
		case "/fixture/worker-effect":
			_ = os.WriteFile(filepath.Join(store, "worker-effect"), []byte("executed"), 0600)
			writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "committed"})
		case "/completion/slash-arguments":
			writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture-model", NoAuth: pointer(false), ModelSelection: &wire.ModelSelection{Current: pointer(true)}}})
		default:
			_ = os.WriteFile(filepath.Join(store, "unexpected-execution-call"), []byte(r.URL.Path), 0600)
			w.WriteHeader(404)
		}
	})}
	_ = server.Serve(l)
	os.Exit(0)
}

type ownedTestOwner struct{}

func (ownedTestOwner) WithdrawWorkerGrants(context.Context) error { return nil }
func (ownedTestOwner) Stop(context.Context) error                 { return nil }
func (ownedTestOwner) SafeIdle(context.Context) error             { return nil }
func TestOwnedCaelisForegroundFencesOnlyPrivateNativeTree(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "caelis-owned-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "caelis")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	body := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestOwnedCaelisProcessHelper$' -- \"$@\"\n"
	if err = os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "owned-watchdog")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestOwnedCaelisWatchdogHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "owned-store")
	s, err := NewOwned(t.Context(), Options{Directory: filepath.Join(dir, "application"), Execution: api.ExecutionSettings{Model: "fixture-model"}}, OwnedHostOptions{NodeID: "owned-node", Binary: binary, Store: store, WatchdogHelper: helper})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(t.Context())
	if err = s.OwnedRuntimeReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	guard := roaming.NewGuard("owned-node", api.NodeCaelis, ownedTestOwner{}, true)
	s.ConfigureExecutionAdmission(guard)
	if err = s.Connect(t.Context()); !errors.Is(err, roaming.ErrFenced) {
		t.Fatal("prepared host created native session without lease", err)
	}
	if _, err = os.Stat(filepath.Join(store, "unexpected-execution-call")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unleased host issued native execution calls", err)
	}
	if _, err = NewOwned(t.Context(), Options{}, OwnedHostOptions{NodeID: "owned-node", Binary: binary, Store: store, WatchdogHelper: helper}); err == nil {
		t.Fatal("second owner adopted foreground store")
	}
	other := exec.Command("/bin/sleep", "60")
	if err = other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	data, err := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err = s.FenceStop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) >= 4*time.Second {
		t.Fatal("foreground hardstop exceeded power budget")
	}
	for _, raw := range strings.Fields(string(data)) {
		pid, _ := strconv.Atoi(raw)
		if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("owned root/child remained", pid, err)
		}
	}
	if err = syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatal("unrelated process targeted", err)
	}
	if err = s.Close(t.Context()); err != nil {
		t.Fatal("postfence close lost confirmation", err)
	}
	if err = New(Options{}).FenceStop(t.Context()); err == nil {
		t.Fatal("shared host gained stop authority")
	}
}

func recordOwnedControl(store, kind string, request any) {
	path := filepath.Join(store, "worker-"+kind+"-count")
	data, _ := os.ReadFile(path)
	count, _ := strconv.Atoi(string(data))
	_ = os.WriteFile(path, []byte(strconv.Itoa(count+1)), 0600)
	data, _ = json.Marshal(request)
	_ = os.WriteFile(filepath.Join(store, "worker-"+kind+"-request"), data, 0600)
}
