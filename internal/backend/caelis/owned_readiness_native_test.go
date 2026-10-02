//go:build (darwin && cgo) || linux

package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

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
	entered := make(chan struct{})
	var once sync.Once
	barrier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/metadata-entered" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		once.Do(func() { close(entered) })
		w.WriteHeader(http.StatusNoContent)
	}))
	defer barrier.Close()
	if err := os.WriteFile(filepath.Join(opts.Store, "fixture-metadata-entered-url"), []byte(barrier.URL+"/metadata-entered"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		out OwnedReadiness
		err error
	}
	results := make(chan result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := ProbeOwnedReadiness(ctx, opts, "")
		results <- result{out, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("cancelled readiness did not finish exact native cleanup")
		}
	})
	// This budget is a failure guard, not the trigger for cancellation. Native
	// metadata must actually enter before the test revokes its original context.
	budget := time.NewTimer(30 * time.Second)
	defer budget.Stop()
	select {
	case <-entered:
	case early := <-results:
		t.Fatal("probe returned before native metadata barrier", early.out, early.err)
	case <-budget.C:
		t.Fatal("native metadata barrier was not reached")
	}
	cancel()
	select {
	case got := <-results:
		if !errors.Is(got.err, context.Canceled) || got.out.Ready {
			t.Fatal("metadata cancellation lost native uncertainty", got.out, got.err)
		}
	case <-budget.C:
		t.Fatal("metadata cancellation did not return a native result")
	}
	assertReadinessStopped(t, opts)
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

func TestOwnedHandshakeFailureCleansDiscoveryBeforeRetry(t *testing.T) {
	options := ownedReadinessFixture(t, "initialize-fail")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if host, err := startOwnedHost(ctx, options); err == nil {
		_ = host.stop(context.Background())
		t.Fatal("failed handshake reported ready")
	}
	discovery := filepath.Join(options.Store, "runtime/service/discovery.json")
	if _, err := os.Lstat(discovery); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed launch retained discovery", err)
	}
	if err := os.WriteFile(filepath.Join(options.Store, "fixture-mode"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	host, err := startOwnedHost(t.Context(), options)
	if err != nil {
		t.Fatal("first handshake failure poisoned Store", err)
	}
	if err = host.stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}
