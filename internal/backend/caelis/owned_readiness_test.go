//go:build darwin || linux

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

// This actual foreground serves only the public initialization/status/catalog
// protocol and retains a tool child for independent shutdown proof. It makes no
// account, model, configuration, application or session requests.
func TestOwnedReadinessProcessHelper(t *testing.T) {
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
	b, _ := json.Marshal(discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: "http://" + l.Addr().String(), InstanceID: "owned-readiness-fixture", PrincipalID: "native-fixture-owner"})
	if os.WriteFile(filepath.Join(store, "runtime/service/discovery.json"), b, 0600) != nil {
		os.Exit(6)
	}
	_ = os.WriteFile(filepath.Join(store, "runtime/service/auth.token"), []byte("SYNTHETIC_TARGET_SERVICE_TOKEN"), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-pids"), []byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(child.Process.Pid)), 0600)
	_ = os.WriteFile(filepath.Join(store, "fixture-home"), []byte(os.Getenv("HOME")), 0600)
	var mu sync.Mutex
	statusCalls := 0
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
		mode, _ := os.ReadFile(filepath.Join(store, "fixture-mode"))
		switch path {
		case "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("native-owned-store"), InstanceId: pointer("owned-readiness-fixture"), Capabilities: required})
		case "/status":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			statusCalls++
			revision := "42"
			if string(mode) == "revision-change" && statusCalls > 1 {
				revision = "43"
			}
			// Mirror pinned Core control/status.StatusModel: false MissingAPIKey
			// is omitted, while Doctor supplies alias/provider/name for configured model.
			out := pinnedReadinessStatusModel{Alias: "provider-config-current", Provider: "mimo", Name: "native-upstream-current", MissingAPIKey: string(mode) == "noauth"}
			if string(mode) == "auth-unconfirmed" {
				out.Provider = ""
				out.Name = ""
			}
			if string(mode) == "no-current" {
				out = pinnedReadinessStatusModel{}
			}
			writeFixture(w, map[string]any{"configuration": map[string]string{"revision": revision}, "model_status": out, "diagnostics": map[string]string{"config_path": "PRIVATE_CONFIG_PATH_MUST_NOT_LEAK"}})

		case "/completion/slash-arguments":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			var req wire.CompletionRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || value(req.Command) != "model" || req.SessionId != nil || req.Cwd != nil {
				w.WriteHeader(400)
				return
			}
			if string(mode) == "metadata-cancel" {
				<-r.Context().Done()
				return
			}
			if string(mode) == "metadata-fault" {
				http.Error(w, "PRIVATE_RESPONSE_MUST_NOT_LEAK", 503)
				return
			}
			// Core modelChoiceCandidates never populates NoAuth; non-reasoning
			// models also omit ModelSelection. Public selectors retain ModelConfigID.
			current := pinnedReadinessCandidate{Value: "native-owned-current", ModelConfigID: "provider-config-current", Display: "Native Current"}
			alternate := pinnedReadinessCandidate{Value: "native-owned-alternate", ModelConfigID: "provider-config-alternate", Display: "Native Alternate"}
			writeFixture(w, []pinnedReadinessCandidate{current, alternate})

		default:
			w.WriteHeader(404)
		}
	})}
	_ = server.Serve(l)
	os.Exit(0)
}

// These field types and omitempty tags mirror pinned public Core e8281aa7,
// rather than generated client pointer fields that would emit false explicitly.
type pinnedReadinessCandidate struct {
	Value          string               `json:"value"`
	Display        string               `json:"display,omitempty"`
	ModelConfigID  string               `json:"model_config_id,omitempty"`
	NoAuth         bool                 `json:"no_auth,omitempty"`
	ModelSelection *wire.ModelSelection `json:"model_selection,omitempty"`
}
type pinnedReadinessStatusModel struct {
	Alias         string `json:"alias,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Name          string `json:"name,omitempty"`
	MissingAPIKey bool   `json:"missing_api_key,omitempty"`
}

func ownedReadinessFixture(t *testing.T, mode string) OwnedHostOptions {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(dir, "caelis")
	helper := filepath.Join(dir, "watchdog")
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestOwnedReadinessProcessHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestOwnedCaelisWatchdogHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := ownedStoreProbeFixture(t)
	if err = os.WriteFile(filepath.Join(store, "fixture-mode"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store, "native-auth-fixture"), []byte("SYNTHETIC_PREEXISTING_TARGET_AUTH"), 0600); err != nil {
		t.Fatal(err)
	}
	return OwnedHostOptions{NodeID: "node-test", Binary: binary, Store: store, WatchdogHelper: helper}
}
func ownedReadinessPIDs(t *testing.T, store string) []int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, s := range strings.Fields(string(b)) {
		p, e := strconv.Atoi(s)
		if e != nil {
			t.Fatal(e)
		}
		pids = append(pids, p)
	}
	if len(pids) != 2 {
		t.Fatal("native root/tool proof missing", pids)
	}
	return pids
}
func assertReadinessStopped(t *testing.T, opts OwnedHostOptions) {
	t.Helper()
	for _, pid := range ownedReadinessPIDs(t, opts.Store) {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("readiness returned before exact native root/tool exit", pid, err)
		}
	}
	if _, err := os.Stat(filepath.Join(opts.Store, "runtime/service/discovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned discovery survived successful stop", err)
	}
	if eligible, reason := ProbeOwnedStore(opts.NodeID, opts.Store); !eligible {
		t.Fatal("confirmed stopped store cannot be probed again", reason)
	}
	b, err := os.ReadFile(filepath.Join(opts.Store, "native-auth-fixture"))
	if err != nil || string(b) != "SYNTHETIC_PREEXISTING_TARGET_AUTH" {
		t.Fatal("target auth modified", err)
	}
	b, err = os.ReadFile(filepath.Join(opts.Store, "fixture-home"))
	if err != nil || string(b) != filepath.Join(opts.Store, ".native-home") {
		t.Fatal("foreground did not use exact private target native home", string(b), err)
	}
	b, err = os.ReadFile(filepath.Join(opts.Store, "fixture-requests"))
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if req != "GET /initialize" && req != "GET /status" && req != "POST /completion/slash-arguments" {
			t.Fatal("readiness called a native mutation", req)
		}
	}
}

func TestOwnedReadinessUsesNativePublicModelAndConfirmedStop(t *testing.T) {
	for _, test := range []struct {
		name, mode, desired, reason string
		ready                       bool
		failure                     bool
	}{
		{name: "current", ready: true}, {name: "durable current identity", desired: "provider-config-current", ready: true}, {name: "non-current auth unconfirmed", desired: "native-owned-alternate", reason: "owned-model-authentication-unconfirmed"}, {name: "no auth", mode: "noauth", reason: "owned-model-authentication-required"}, {name: "unknown auth", mode: "auth-unconfirmed", reason: "owned-model-authentication-unconfirmed"}, {name: "missing current", mode: "no-current", reason: "owned-current-model-unavailable"}, {name: "foreign source model", desired: "local-codex-model", reason: "owned-model-unavailable"}, {name: "public catalog fault", mode: "metadata-fault", reason: "owned-metadata-unavailable", failure: true}, {name: "configuration changes", mode: "revision-change", reason: "owned-configuration-changed", failure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := ownedReadinessFixture(t, test.mode)
			out, err := ProbeOwnedReadiness(t.Context(), opts, test.desired)
			if out.Ready != test.ready || out.Reason != test.reason || (err != nil) != test.failure {
				t.Fatal(out, err)
			}
			if test.mode != "metadata-fault" && test.mode != "revision-change" && out.NativeConfigRevision != "42" {
				t.Fatal("native configuration revision lost", out)
			}
			if test.mode != "metadata-fault" && test.mode != "revision-change" && test.mode != "no-current" && out.CurrentModel != "native-owned-current" {
				t.Fatal("current native model replaced", out)
			}
			if test.ready && strings.Join(out.AuthenticatedModels, ",") != "native-owned-current,provider-config-current" {
				t.Fatal("public authenticated candidate set lost", out.AuthenticatedModels)
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), opts.Store) || strings.Contains(string(b), "PRIVATE_") || strings.Contains(string(b), "SYNTHETIC_") || err != nil && strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("private readiness data exposed", string(b), err)
			}
			assertReadinessStopped(t, opts)
		})
	}
}
func TestOwnedReadinessDoesNotInitializeOrAdoptStores(t *testing.T) {
	for _, kind := range []string{"missing", "unmarked", "wrong-node", "discovery", "default"} {
		t.Run(kind, func(t *testing.T) {
			opts := ownedReadinessFixture(t, "")
			switch kind {
			case "missing":
				opts.Store = filepath.Join(opts.Store, "missing")
			case "unmarked":
				if err := os.Remove(filepath.Join(opts.Store, ".caelis-bot-node-owner.json")); err != nil {
					t.Fatal(err)
				}
			case "wrong-node":
				opts.NodeID = "other-node"
			case "discovery":
				if err := os.MkdirAll(filepath.Join(opts.Store, "runtime/service"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(opts.Store, "runtime/service/discovery.json"), []byte("preexisting foreign discovery"), 0600); err != nil {
					t.Fatal(err)
				}
			case "default":
				t.Setenv("HOME", opts.Store)
				opts.Store = filepath.Join(opts.Store, ".caelis")
			}
			out, err := ProbeOwnedReadiness(t.Context(), opts, "")
			if err != nil || out.Ready || out.Reason == "" {
				t.Fatal(out, err)
			}
			if _, err := os.Stat(filepath.Join(opts.Store, "fixture-pids")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("ineligible store launched", err)
			}
			if kind == "missing" || kind == "default" {
				if _, err := os.Stat(opts.Store); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("probe initialized missing store", err)
				}
			}
		})
	}
}
func TestOwnedReadinessFaultStopsItsTreeAndKeepsOtherOwnerLive(t *testing.T) {
	other := ownedReadinessFixture(t, "")
	host, err := startOwnedHost(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := host.stop(stop); err != nil {
			t.Error(err)
		}
	})
	opts := ownedReadinessFixture(t, "metadata-fault")
	out, err := ProbeOwnedReadiness(t.Context(), opts, "")
	if err == nil || out.Ready || out.Reason != "owned-metadata-unavailable" {
		t.Fatal("failed native readiness hidden", out, err)
	}
	assertReadinessStopped(t, opts)
	if !host.process.Live() || host.ready(t.Context(), "") != nil {
		t.Fatal("readiness fault stopped other exact owner")
	}
	for _, pid := range ownedReadinessPIDs(t, other.Store) {
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatal("other native root/tool stopped", pid, err)
		}
	}
	// An already cancelled caller never launches a foreground or initializes state.
	cancelled := ownedReadinessFixture(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err = ProbeOwnedReadiness(ctx, cancelled, "")
	if !errors.Is(err, context.Canceled) || out.Ready {
		t.Fatal("cancelled probe lost original error", out, err)
	}
	if _, err := os.Stat(filepath.Join(cancelled.Store, "fixture-pids")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled probe launched", err)
	}
}

func TestOwnedReadinessCancelledMetadataStillStopsExactForeground(t *testing.T) {
	opts := ownedReadinessFixture(t, "metadata-cancel")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	out, err := ProbeOwnedReadiness(ctx, opts, "")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || out.Ready {
		t.Fatal("metadata cancellation lost native uncertainty", out, err)
	}
	assertReadinessStopped(t, opts)
}
func TestOwnedReadinessExistingStartupNeverCreatesMissingStore(t *testing.T) {
	opts := ownedReadinessFixture(t, "")
	opts.Store = filepath.Join(opts.Store, "missing")
	if _, err := startOwnedHostWithStore(t.Context(), opts, true); err == nil {
		t.Fatal("required existing store was initialized")
	}
	if _, err := os.Stat(opts.Store); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("existing-only startup created missing store", err)
	}
}

func TestOwnedReadinessPinnedCoreOmittedFalseIsRealCurrentAuthMetadata(t *testing.T) {
	candidate, _ := json.Marshal(pinnedReadinessCandidate{Value: "native-owned-current", ModelConfigID: "provider-config-current"})
	status, _ := json.Marshal(pinnedReadinessStatusModel{Alias: "provider-config-current", Provider: "mimo", Name: "native-upstream-current"})
	if strings.Contains(string(candidate), "no_auth") || strings.Contains(string(candidate), "model_selection") || strings.Contains(string(status), "missing_api_key") {
		t.Fatal("fixture does not reflect native omitted bool encoding", string(candidate), string(status))
	}
	var projected wire.SlashArgCandidate
	var model wire.StatusModel
	if json.Unmarshal(candidate, &projected) != nil || json.Unmarshal(status, &model) != nil || projected.NoAuth != nil || projected.ModelSelection != nil || model.MissingApiKey != nil {
		t.Fatal("fixture invented explicit positive flags")
	}
	opts := ownedReadinessFixture(t, "")
	host, err := startOwnedHost(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := host.stop(stop); err != nil {
			t.Error(err)
		}
	}()
	if err := host.ready(t.Context(), ""); err != nil {
		t.Fatal("actual owned preparation still rejects non-reasoning current model", err)
	}
}
