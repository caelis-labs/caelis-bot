package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func TestOwnedRuntimeProbeUsesCurrentNativeBindingWithoutAdoption(t *testing.T) {
	s := agentFixture(t)
	binary := ownedSettingsBinary(t, s.options.Directory, "caelis")
	store, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0700); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) {
		reads.Add(1)
		return OwnedRuntimeSettings{Backend: api.NodeCaelis, Binary: binary, Store: store}, nil
	}
	probe, err := s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCaelis)
	if !codex.OwnedRuntimeSupported() {
		if err != nil || probe.Eligible || probe.Reason != "unsupported-platform" || reads.Load() != 0 {
			t.Fatal("unsupported host became ownable", probe, err)
		}
		probe, err = s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCodex)
		if err != nil || probe.Eligible || probe.Reason != "unsupported-platform" || reads.Load() != 0 {
			t.Fatal("unsupported Codex probe read metadata or claimed ownership", probe, err)
		}
		if entries, err := os.ReadDir(store); err != nil || len(entries) != 0 {
			t.Fatal("unsupported query changed Store", entries, err)
		}
		return
	}
	if err != nil || probe.Eligible || probe.Reason != "owned-store-setup-required" {
		t.Fatal("unmarked existing Store became ownable", probe, err)
	}
	if entries, err := os.ReadDir(store); err != nil || len(entries) != 0 {
		t.Fatal("query adopted unmarked Store", entries, err)
	}
	marker := filepath.Join(store, ".caelis-bot-node-owner.json")
	if err := os.WriteFile(marker, []byte(`{"nodeId":"node-test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	probe, err = s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || !probe.Eligible || probe.Reason != "" {
		t.Fatal("explicit target ownership marker not honored", probe, err)
	}
	store = filepath.Join(store, "new-native-binding")
	probe, err = s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCaelis)
	if err != nil || probe.Eligible || probe.Reason != "owned-store-setup-required" || reads.Load() != 3 {
		t.Fatal("stale native binding reused by probe", probe, err, reads.Load())
	}
	data, _ := json.Marshal(probe)
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 2 || fields["eligible"] != false || fields["reason"] != probe.Reason {
		t.Fatal("ownability DTO acquired native paths or runtime state", fields, err)
	}
}

func TestOwnedRuntimeProbeCodexOwnershipIsSeparateFromHealthAndAuthentication(t *testing.T) {
	s := agentFixture(t)
	s.options.Binaries[api.NodeCodex] = ownedSettingsBinary(t, s.options.Directory, "codex")
	var healthReads atomic.Int32
	s.options.Health = func(context.Context, api.NodeBackend) (NativeHealth, error) {
		healthReads.Add(1)
		return NativeHealth{}, nil
	}
	probe, err := s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCodex)
	supported := codex.OwnedRuntimeSupported()
	if err != nil || probe.Eligible != supported || healthReads.Load() != 0 {
		t.Fatal("ownability claimed authentication/health or unsupported execution", probe, err)
	}
	s.options.Binaries[api.NodeCodex] = filepath.Join(s.options.Directory, "missing-binary")
	probe, err = s.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || probe.Eligible || supported && probe.Reason != "runtime-metadata-unavailable" || !supported && probe.Reason != "unsupported-platform" {
		t.Fatal("absent binary became ownable", probe, err)
	}
}

func TestOwnedRuntimeProbeClosedInputsAndPinnedNativeIPC(t *testing.T) {
	s := agentFixture(t)
	s.options.Binaries[api.NodeCodex] = ownedSettingsBinary(t, s.options.Directory, "codex")
	for _, body := range []string{`{"nodeId":"node-test","backend":"codex","store":"/arbitrary"}`, `{"nodeId":"node-test","backend":"codex","eligible":true}`, `{"nodeId":"foreign-node","backend":"codex"}`, `{"nodeId":"node-test","backend":"unknown"}`} {
		response := httptest.NewRecorder()
		Handler(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/node/probe-owned-runtime", bytes.NewBufferString(body)))
		if response.Code == http.StatusOK {
			t.Fatal("unknown input acquired ownability authority")
		}
	}
	local, remote := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- productrpc.ServeNativeStream(t.Context(), remote, remote, Handler(s), allowed) }()
	c, err := NewClient(s.options.NodeID, local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); <-done }()
	probe, err := c.ProbeOwnedRuntime(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || probe.Eligible != (codex.OwnedRuntimeSupported()) {
		t.Fatal("closed ownability IPC failed", probe, err)
	}
	if _, err := c.ProbeOwnedRuntime(t.Context(), "foreign-node", api.NodeCodex); err == nil {
		t.Fatal("client repinned probe identity")
	}
	if _, err := c.ProbeOwnedRuntime(t.Context(), s.options.NodeID, "unknown"); err == nil {
		t.Fatal("client widened probe backend")
	}
}

func TestOwnedRuntimeProbeClientRejectsExpandedOrArbitraryEvidence(t *testing.T) {
	for _, response := range []string{`{"eligible":true,"reason":"owned-store-setup-required"}`, `{"eligible":false,"reason":""}`, `{"eligible":false,"reason":"/target/private-store"}`, `{"eligible":true,"reason":"","store":"/target/private-store"}`} {
		t.Run(response, func(t *testing.T) {
			local, remote := net.Pipe()
			done := make(chan error, 1)
			go func() {
				done <- productrpc.ServeNativeStream(t.Context(), remote, remote, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }), allowed)
			}()
			c, err := NewClient("node-test", local)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close(); <-done }()
			if _, err := c.ProbeOwnedRuntime(t.Context(), "node-test", api.NodeCaelis); err == nil {
				t.Fatal("arbitrary or expanded ownability evidence accepted")
			}
		})
	}
}
