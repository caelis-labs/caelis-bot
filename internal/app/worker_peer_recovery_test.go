//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/coder/websocket"
)

type foreignWorkerFixtureLaunch struct {
	PID      int    `json:"pid"`
	Endpoint string `json:"endpoint"`
	Stop     string `json:"stop"`
}

// This is a test-only native CLI, launched by the production attachable-process
// path. It serves only initialize/account reads and cannot call any model.
func TestForeignWorkerNativeSocketChild(t *testing.T) {
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return
	}
	if len(args) != 4 || args[1] != "app-server" || args[2] != "--listen" || !strings.HasPrefix(args[3], "unix://") {
		os.Exit(2)
	}
	journal, endpoint := args[0], args[3]
	listener, err := net.Listen("unix", strings.TrimPrefix(endpoint, "unix://"))
	if err != nil {
		os.Exit(3)
	}
	launch := foreignWorkerFixtureLaunch{PID: os.Getpid(), Endpoint: endpoint, Stop: fmt.Sprintf("%s.stop-%d", journal, os.Getpid())}
	f, err := os.OpenFile(journal, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil || json.NewEncoder(f).Encode(launch) != nil {
		os.Exit(4)
	}
	_ = f.Close()
	go func() {
		for {
			if _, err := os.Stat(launch.Stop); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		conn := websocket.NetConn(r.Context(), ws, websocket.MessageText)
		defer conn.Close()
		decode, encode := json.NewDecoder(conn), json.NewEncoder(conn)
		for {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if decode.Decode(&request) != nil {
				return
			}
			if len(request.ID) == 0 {
				continue
			}
			var result any
			switch request.Method {
			case "initialize":
				result = map[string]any{"userAgent": "app-worker-recovery-fixture"}
			case "account/read":
				result = map[string]any{"account": map[string]any{"type": "apiKey"}, "requiresOpenaiAuth": true}
			default:
				_ = encode.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32601, "message": "not a fixture read"}})
				continue
			}
			if encode.Encode(map[string]any{"id": request.ID, "result": result}) != nil {
				return
			}
		}
	})}
	_ = server.Serve(listener)
	os.Exit(0)
}

func foreignWorkerFixtureLaunches(t *testing.T, journal string) []foreignWorkerFixtureLaunch {
	t.Helper()
	f, err := os.Open(journal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var launches []foreignWorkerFixtureLaunch
	decode := json.NewDecoder(f)
	for decode.More() {
		var launch foreignWorkerFixtureLaunch
		if err := decode.Decode(&launch); err != nil {
			t.Fatal(err)
		}
		launches = append(launches, launch)
	}
	return launches
}

// Observe only the journal-owned fixture process. Do not reap the production
// launcher's child or send a signal; the native recovery path still verifies
// its own original PID and birth before it may prepare another owner.
func foreignWorkerFixtureExited(ctx context.Context, pid int) (bool, error) {
	if pid <= 1 {
		return false, fmt.Errorf("invalid fixture PID")
	}
	out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	state := strings.TrimSpace(string(out))
	if err != nil {
		if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() != 1 || state != "" || len(exited.Stderr) != 0 {
			return false, err
		}
	}
	return state == "" || strings.HasPrefix(state, "Z"), nil
}

func TestForeignWorkerFactoryRetainsFailedOwnerAndPublishesRecoveredEndpoint(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	fixtureRoot := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(fixtureRoot, "empty-native-home"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(fixtureRoot, "launches.jsonl")
	binary := filepath.Join(fixtureRoot, "codex-fixture")
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nexec /usr/bin/env -i PATH=/usr/bin:/bin " + quote(executable) + " -test.run='^TestForeignWorkerNativeSocketChild$' -- " + quote(journal) + " \"$@\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := backend.SaveRuntimeProfile(root, api.RuntimeSettings{Runtime: "codex", CLIPath: binary}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = a.closeWorkerPeer() // Detach the production observer before fixture exit.
		launches := foreignWorkerFixtureLaunches(t, journal)
		for _, launch := range launches {
			_ = os.WriteFile(launch.Stop, nil, 0600)
		}
		for _, launch := range launches {
			path := strings.TrimPrefix(launch.Endpoint, "unix://")
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				conn, err := net.DialTimeout("unix", path, 20*time.Millisecond)
				if err != nil {
					break
				}
				_ = conn.Close()
				time.Sleep(10 * time.Millisecond)
			}
			// The journal was emitted by this test-only owned child, and the
			// production launcher generated this exact private socket directory.
			if filepath.Base(path) == "runtime.sock" && strings.HasPrefix(filepath.Base(filepath.Dir(path)), "caelis-bot-") {
				_ = os.RemoveAll(filepath.Dir(path))
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const source = "fixture-source-a"
	sourceRoot := filepath.Join(root, "WorkerPeers", "owners", source)
	owner, runtime, _, err := a.openForeignWorker(ctx, source, "codex", sourceRoot)
	if err != nil || runtime != "codex" {
		t.Fatal("construct isolated owner", runtime, err)
	}
	native, ok := owner.(*foreignCodexOwner)
	if !ok {
		t.Fatal("factory did not expose recoverable native owner")
	}
	if len(foreignWorkerFixtureLaunches(t, journal)) != 0 {
		t.Fatal("factory connected before retaining recoverable owner")
	}
	if err := native.Connect(ctx); err != nil {
		t.Fatal("empty readiness", err)
	}
	launches := foreignWorkerFixtureLaunches(t, journal)
	if len(launches) != 1 {
		t.Fatal("initial readiness started unexpected owners", launches)
	}
	readEndpoint := func() string {
		t.Helper()
		var saved struct{ Endpoint string }
		data, err := os.ReadFile(native.endpointPath)
		if err != nil || json.Unmarshal(data, &saved) != nil {
			t.Fatal("actual endpoint cache unavailable", err)
		}
		return saved.Endpoint
	}
	if readEndpoint() != launches[0].Endpoint {
		t.Fatal("Connect did not publish actual native endpoint")
	}
	original := launches[0]
	originalCache, err := os.ReadFile(native.endpointPath)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalOwner := func() {
		t.Helper()
		observed := foreignWorkerFixtureLaunches(t, journal)
		if len(observed) != 1 || observed[0] != original {
			t.Fatal("ordinary readiness replaced the original fixture owner", observed)
		}
		cached, err := os.ReadFile(native.endpointPath)
		if err != nil || !bytes.Equal(cached, originalCache) {
			t.Fatal("ordinary readiness changed original endpoint evidence", err)
		}
	}
	if err := os.WriteFile(launches[0].Stop, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Wait for the test-only native server to close its original socket. The
	// established transport's asynchronous reader may not yet have consumed EOF.
	for {
		conn, err := net.DialTimeout("unix", strings.TrimPrefix(launches[0].Endpoint, "unix://"), 20*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		select {
		case <-ctx.Done():
			t.Fatal("fixture owner did not exit")
		case <-time.After(10 * time.Millisecond):
		}
	}
	readinessDeadline := time.Now().Add(5 * time.Second)
	cachedReadiness := 0
	for {
		err := native.Connect(ctx)
		// A short-lived cached ready result is not replacement evidence. Check
		// both process launches and exact persisted endpoint on every outcome;
		// any actual replacement still fails immediately, including nil results.
		assertOriginalOwner()
		if err != nil {
			break
		}
		cachedReadiness++
		if time.Now().After(readinessDeadline) {
			t.Fatal("ordinary readiness did not observe the exited original owner")
		}
		select {
		case <-ctx.Done():
			t.Fatal("ordinary readiness observation timed out", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if cachedReadiness > 0 {
		t.Log("cached ready results before native reader observed exit:", cachedReadiness)
	}
	exitCtx, exitCancel := context.WithTimeout(ctx, 3*time.Second)
	defer exitCancel()
	for {
		exited, err := foreignWorkerFixtureExited(exitCtx, original.PID)
		assertOriginalOwner()
		if err != nil {
			t.Fatal("could not confirm fixture process exit", err)
		}
		if exited {
			break
		}
		select {
		case <-exitCtx.Done():
			t.Fatal("fixture process did not exit", exitCtx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	memo, _, _, err := a.openForeignWorker(ctx, source, "codex", sourceRoot)
	if err != nil || memo != owner || readEndpoint() != launches[0].Endpoint {
		t.Fatal("failed readiness discarded recovery owner or its evidence", err)
	}
	assertOriginalOwner()
	if err := native.RecoverNewWork(ctx); err != nil {
		t.Fatal("explicit recovery through retained app owner", err)
	}
	launches = foreignWorkerFixtureLaunches(t, journal)
	if len(launches) != 2 || launches[0].PID == launches[1].PID || launches[0].Endpoint == launches[1].Endpoint {
		t.Fatal("explicit recovery did not prepare one independent owner", launches)
	}
	actual, err := native.NativeEndpoint()
	if err != nil || readEndpoint() != actual || actual != launches[1].Endpoint {
		t.Fatal("recovery did not replace endpoint cache with actual listener", actual, err)
	}
	if err := native.Connect(ctx); err != nil || len(foreignWorkerFixtureLaunches(t, journal)) != 2 {
		t.Fatal("ordinary readiness replaced recovered owner", err)
	}
}
