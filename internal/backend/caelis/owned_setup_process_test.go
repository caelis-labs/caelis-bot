//go:build (darwin && cgo) || linux

package caelis

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// The foreground models an empty configured Store: initialization works before
// authentication, while model/application/session methods are unavailable.
// A retained real tool child makes shutdown require native descendant proof.
func TestOwnedSetupProcessHelper(t *testing.T) {
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
	child := exec.Command("/bin/sleep", "600")
	if child.Start() != nil {
		os.Exit(4)
	}
	if os.MkdirAll(filepath.Join(store, "runtime/service"), 0700) != nil {
		os.Exit(5)
	}
	b, _ := json.Marshal(discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: "http://" + l.Addr().String(), InstanceID: "owned-empty-setup-fixture", PrincipalID: "native-fixture-owner"})
	if os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), b, 0600) != nil {
		os.Exit(6)
	}
	_ = os.WriteFile(filepath.Join(store, "runtime/service/auth.token"), []byte("SYNTHETIC_TARGET_SERVICE_TOKEN"), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-pids"), []byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(child.Process.Pid)), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-home"), []byte(os.Getenv("HOME")), 0600)
	var mu sync.Mutex
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		requests, _ := os.OpenFile(filepath.Join(store, "fixture-requests"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if requests != nil {
			_, _ = requests.WriteString(r.Method + " " + path + "\n")
			_ = requests.Close()
		}
		if r.Header.Get("Authorization") != "Bearer SYNTHETIC_TARGET_SERVICE_TOKEN" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "GET" && path == "/initialize" {
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("empty-native-owned-store"), InstanceId: pointer("owned-empty-setup-fixture"), Capabilities: required})
			return
		}
		http.Error(w, "empty setup has no application/model/session", 404)
	})}
	_ = server.Serve(l)
	os.Exit(0)
}

func ownedSetupFixture(t *testing.T) OwnedHostOptions {
	t.Helper()
	opts := ownedReadinessFixture(t, "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err = os.WriteFile(opts.Binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestOwnedSetupProcessHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// Empty target has no provider configuration or model authentication at all.
	if err = os.Remove(filepath.Join(opts.Store, "native-auth-fixture")); err != nil {
		t.Fatal(err)
	}
	return opts
}

func assertSetupOnlyInitialized(t *testing.T, opts OwnedHostOptions) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(opts.Store, "fixture-requests"))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		t.Fatal("public initialization missing", err)
	}
	for _, req := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if req != "GET /initialize" {
			t.Fatal("setup required auth or dispatched native mutation", req)
		}
	}
	for _, p := range []string{"native-auth-fixture", "worker-app", "worker-task", "application.json"} {
		if _, err = os.Stat(filepath.Join(opts.Store, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("setup initialized application or model credentials", p, err)
		}
	}
}

func assertSetupPIDsStopped(t *testing.T, opts OwnedHostOptions) {
	t.Helper()
	for _, pid := range ownedReadinessPIDs(t, opts.Store) {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("setup returned before exact native root/tool exit", pid, err)
		}
	}
}

func closeSetup(t *testing.T, s *OwnedSetup) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Error(err)
	}
}

func TestOwnedSetupEmptyStoreOnlyInitializesAndConfirmsClose(t *testing.T) {
	opts := ownedSetupFixture(t)
	before := time.Now()
	s, err := BeginOwnedSetup(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSetup(t, s) })
	if s.deadline.Before(before.Add(ownedSetupLifetime)) || s.deadline.After(time.Now().Add(ownedSetupLifetime)) || s.epoch == "" {
		t.Fatal("setup lifetime has no bounded native watchdog generation")
	}
	settings, err := s.SetupSettings(t.Context())
	if err != nil || settings.CaelisStore != opts.Store || settings.CLIPath != opts.Binary || settings.Runtime != "caelis" {
		t.Fatal("setup did not preserve target-native SDK settings", err)
	}
	if _, err = BeginOwnedSetup(t.Context(), opts); err == nil {
		t.Fatal("live owned foreground was adopted by another wizard")
	}
	if err = s.Check(t.Context()); err != nil {
		t.Fatal("failed second Begin stopped original owner", err)
	}
	assertSetupOnlyInitialized(t, opts)
	closeSetup(t, s)
	assertSetupPIDsStopped(t, opts)
	if _, err = s.SetupSettings(t.Context()); !errors.Is(err, ErrOwnedSetupClosed) {
		t.Fatal("closed setup settings remained usable", err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Close completed without Done")
	}
	if eligible, reason := ProbeOwnedStore(opts.NodeID, opts.Store); !eligible {
		t.Fatal("confirmed Close did not release exact store", reason)
	}
}

func TestOwnedSetupOwnerCancellationAndDeadlineStopExactProcesses(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(strconv.FormatBool(deadline), func(t *testing.T) {
			opts := ownedSetupFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			}
			defer cancel()
			s, err := BeginOwnedSetup(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeSetup(t, s) })
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
				if d, _ := ctx.Deadline(); !s.deadline.Equal(d) {
					t.Fatal("watchdog bound ignored earlier owner deadline")
				}
			} else {
				cancel()
			}
			select {
			case <-s.Done():
			case <-time.After(9 * time.Second):
				t.Fatal("setup owner cancellation left native foreground alive")
			}
			closeSetup(t, s)
			if err := s.Check(t.Context()); !errors.Is(err, ErrOwnedSetupClosed) || !errors.Is(err, want) {
				t.Fatal("expired setup lost original cancellation", err)
			}
			assertSetupPIDsStopped(t, opts)
			assertSetupOnlyInitialized(t, opts)
		})
	}
}

func TestOwnedSetupStaleDiscoveryFencesOnlyOriginalOwner(t *testing.T) {
	opts, otherOpts := ownedSetupFixture(t), ownedSetupFixture(t)
	s, err := BeginOwnedSetup(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSetup(t, s) })
	other, err := BeginOwnedSetup(context.Background(), otherOpts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSetup(t, other) })
	discoveryPath := filepath.Join(opts.Store, "runtime/service/discovery.json")
	b, err := os.ReadFile(discoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	var d discovery
	if err = json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	d.InstanceID = "replacement-must-not-be-adopted"
	b, _ = json.Marshal(d)
	if err = os.WriteFile(discoveryPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetupSettings(t.Context()); err == nil {
		t.Fatal("setup used replacement discovery")
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stale setup did not hard-fence original owner")
	}
	assertSetupPIDsStopped(t, opts)
	if _, err := os.Stat(discoveryPath); err != nil {
		t.Fatal("original owner removed replacement discovery", err)
	}
	if err := other.Check(t.Context()); err != nil {
		t.Fatal("unrelated owner stopped by stale setup", err)
	}
	for _, pid := range ownedReadinessPIDs(t, otherOpts.Store) {
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatal("unrelated native root/tool stopped", pid, err)
		}
	}
	assertSetupOnlyInitialized(t, opts)
	assertSetupOnlyInitialized(t, otherOpts)
}

func TestOwnedSetupRetainsActualCleanupError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission failure fixture")
	}
	opts := ownedSetupFixture(t)
	s, err := BeginOwnedSetup(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(opts.Store, "runtime/service")
	t.Cleanup(func() { _ = os.Chmod(service, 0700) })
	if err := os.Chmod(service, 0500); err != nil {
		t.Fatal(err)
	}
	stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err = s.Close(stop)
	if err == nil || !errors.Is(err, syscall.EACCES) || err.Error() != "owned-setup-stop-unconfirmed" {
		t.Fatal("cleanup uncertainty disappeared", err)
	}
	if repeated := s.Close(t.Context()); repeated != err {
		t.Fatal("Close replaced original cleanup error", repeated, err)
	}
	assertSetupPIDsStopped(t, opts)
	if err := s.Check(t.Context()); !errors.Is(err, ErrOwnedSetupClosed) {
		t.Fatal("failed cleanup reopened native setup", err)
	}
	assertSetupOnlyInitialized(t, opts)
}

func TestSessionOwnedSetupSettingsDelegatesExactLiveHostWithoutEnrollment(t *testing.T) {
	opts := ownedSetupFixture(t)
	setup, err := BeginOwnedSetup(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSetup(t, setup) })
	s := &Session{owned: setup.host}
	settings, err := s.OwnedSetupSettings(t.Context())
	if err != nil || settings.CaelisStore != opts.Store {
		t.Fatal("owned native setup settings unavailable", err)
	}
	assertSetupOnlyInitialized(t, opts)
	closeSetup(t, setup)
	if _, err := s.OwnedSetupSettings(t.Context()); err == nil {
		t.Fatal("stopped owned Host still delegated setup")
	}
}
