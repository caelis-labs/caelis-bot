package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// The child substitutes only SSH transport/bootstrap. Native enrollment,
// persisted credentials, HTTP handshake and reconnect use production code.
func TestSSHRestartTransportHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--ssh-restart-fixture" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	directory := os.Args[index+1]
	args := os.Args[index+2:]
	for i, arg := range args {
		if arg == "-W" && i+1 < len(args) {
			conn, err := net.Dial("tcp", args[i+1])
			if err != nil {
				os.Exit(2)
			}
			defer conn.Close()
			go func() { io.Copy(conn, os.Stdin); conn.(*net.TCPConn).CloseWrite() }()
			io.Copy(os.Stdout, conn)
			os.Exit(0)
		}
	}
	var req caelis.WorkerBootstrapRequest
	if json.NewDecoder(os.Stdin).Decode(&req) != nil {
		os.Exit(3)
	}
	data, err := os.ReadFile(filepath.Join(directory, "ready.json"))
	if err != nil {
		os.Exit(4)
	}
	var ready caelis.WorkerBootstrapResult
	if json.Unmarshal(data, &ready) != nil {
		os.Exit(5)
	}
	if req.Action != "probe" && (req.StoreID != ready.StoreID || req.InstanceID != ready.InstanceID || req.PrincipalID != ready.PrincipalID) {
		os.Exit(6)
	}
	switch req.Action {
	case "probe":
	case "enroll":
		if os.WriteFile(filepath.Join(directory, "credential"), []byte(req.AppCredential), 0600) != nil {
			os.Exit(7)
		}
	case "resolve_workspace", "prepare_workspace":
		ready.Workspace = "/synthetic/workspace"
	default:
		os.Exit(8)
	}
	if json.NewEncoder(os.Stdout).Encode(ready) != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

func TestSameSSHWorkerReconnectsAcrossHostInstances(t *testing.T) {
	directory := t.TempDir()
	var instance atomic.Value
	instance.Store("instance-1")
	var appID atomic.Value
	appID.Store("worker-app")
	connection := func() wire.ApplicationConnection {
		return wire.ApplicationConnection{ApplicationId: appID.Load().(string), ConnectionId: "worker-connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)}
	}
	capabilities := []string{"shared-native-workers-v1", "turn-steering-receipts-v1", "application-runtime-v1", "application-resource-transfer-v1"}
	store := "store"
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret, err := os.ReadFile(filepath.Join(directory, "credential"))
		if err != nil || r.Header.Get("Authorization") != "Bearer "+string(secret) {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/initialize":
			current := instance.Load().(string)
			json.NewEncoder(w).Encode(wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: &store, InstanceId: &current, Capabilities: capabilities})
		case "/application/connection":
			json.NewEncoder(w).Encode(connection())
		default:
			t.Errorf("unexpected native endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer host.Close()
	ready := caelis.WorkerBootstrapResult{Endpoint: host.URL, StoreID: store, InstanceID: instance.Load().(string), PrincipalID: "owner", Capabilities: capabilities, ModelConfigured: true, ModelAuth: "reported_ready", Execution: api.WorkExecutionSettings{Model: "fixture"}}
	publish := func() {
		t.Helper()
		life := connection()
		ready.Connection = &life
		data, _ := json.Marshal(ready)
		if err := os.WriteFile(filepath.Join(directory, "next.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(directory, "next.json"), filepath.Join(directory, "ready.json")); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "ssh")
	script := "#!/bin/sh\nexec " + sshQuote(executable) + " -test.run '^TestSSHRestartTransportHelper$' -- --ssh-restart-fixture " + sshQuote(directory) + " \"$@\"\n"
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	transport, err := NewSSHWorker(SSHConfig{Target: "fixture", Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	worker := caelis.NewWorker(caelis.WorkerOptions{Directory: filepath.Join(directory, "worker"), Target: api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}, Endpoint: transport.Endpoint, Workspace: transport})
	defer worker.Close(context.Background())
	if err = worker.Connect(t.Context()); err != nil || !worker.Ready() {
		t.Fatal("initial native enrollment", err)
	}
	credential, err := os.ReadFile(filepath.Join(directory, "worker", "worker-application-credential.json"))
	if err != nil {
		t.Fatal(err)
	}
	for generation := 2; generation <= 3; generation++ {
		instance.Store(fmt.Sprintf("instance-%d", generation))
		ready.InstanceID = instance.Load().(string)
		publish()
		if err = worker.Reconnect(t.Context()); err != nil || !worker.Ready() {
			t.Fatal("same object native reconnect", generation, err)
		}
		current, _ := os.ReadFile(filepath.Join(directory, "worker", "worker-application-credential.json"))
		if string(current) != string(credential) {
			t.Fatal("restart replaced durable credential")
		}
		if path, err := worker.ResolveWorkWorkspace(t.Context(), "task", ""); err != nil || path != "/synthetic/workspace" {
			t.Fatal("workspace request retained stale instance", path, err)
		}
	}
	// A new probe must still agree with enrollment and the tunneled initialize.
	old, err := transport.Endpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	instance.Store("instance-4")
	ready.InstanceID = "instance-4"
	publish()
	if _, err = old.Enroll(t.Context(), "same-probe", "synthetic"); err == nil {
		t.Fatal("old enrollment adopted a new probe identity")
	}
	ready.InstanceID = "different-from-http"
	publish()
	if err = worker.Reconnect(t.Context()); err == nil || worker.Ready() {
		t.Fatal("tunnel disagreed with probe but connected")
	}
	ready.InstanceID = instance.Load().(string)
	publish()
	appID.Store("foreign-app")
	if err = worker.Reconnect(t.Context()); err == nil {
		t.Fatal("changed application adopted old credentials")
	}
	appID.Store("worker-app")
	for _, field := range []string{"store", "principal"} {
		original := ready
		if field == "store" {
			ready.StoreID = "foreign-store"
		} else {
			ready.PrincipalID = "foreign-owner"
		}
		publish()
		if _, err = transport.Endpoint(t.Context()); err == nil {
			t.Fatal("persistent identity changed", field)
		}
		ready = original
		publish()
	}
}
