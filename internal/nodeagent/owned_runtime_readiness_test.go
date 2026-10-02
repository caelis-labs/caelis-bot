//go:build darwin || linux

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
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func readinessFixture(t *testing.T) (*Service, OwnedRuntimeReadinessRequest, *atomic.Int32) {
	t.Helper()
	s := agentFixture(t)
	store, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, ".caelis-bot-node-owner.json"), []byte(`{"nodeId":"node-test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := ownedSettingsBinary(t, s.options.Directory, "caelis")
	helper := ownedSettingsBinary(t, s.options.Directory, "trusted-node-helper")
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) {
		return OwnedRuntimeSettings{Backend: api.NodeCaelis, Binary: binary, Store: store}, nil
	}
	s.options.OwnedRuntimeCompanion = func(context.Context) (OwnedRuntimeCompanion, error) {
		return OwnedRuntimeCompanion{Path: helper, SHA256: hex.EncodeToString(hash[:])}, nil
	}
	request := OwnedRuntimeReadinessRequest{NodeID: s.options.NodeID, Backend: api.NodeCaelis, OperationID: "approved-enable-native-readiness", ExpectedBinary: binary, ExpectedStore: store}
	calls := new(atomic.Int32)
	s.readinessCheck = func(ctx context.Context, options caelis.OwnedHostOptions, model string) (caelis.OwnedReadiness, error) {
		calls.Add(1)
		var intent ownedRuntimeReadinessRecord
		if err := readPrivateJSON(filepath.Join(s.readinessDirectory(request.Backend, request.OperationID), "intent.json"), &intent); err != nil || intent.Request != request || intent.Receipt.Outcome != "unknown" {
			t.Error("Host admission preceded original durable intent", intent, err)
		}
		if options.NodeID != request.NodeID || options.Binary != binary || options.Store != store || options.WatchdogHelper != helper || model != request.Model {
			t.Error("readiness widened frozen native authority", options)
		}
		return caelis.OwnedReadiness{Ready: true, CurrentModel: "target-current", NativeConfigRevision: "public-r1", AuthenticatedModels: []string{"target-current"}}, nil
	}
	return s, request, calls
}

func TestOwnedReadinessOriginalIntentReplayAndRestartNeverStartsAnotherHost(t *testing.T) {
	s, request, calls := readinessFixture(t)
	if _, err := s.ProbeOwnedRuntime(t.Context(), request.NodeID, request.Backend); err != nil || calls.Load() != 0 {
		t.Fatal("read-only probe ran readiness mutation", err)
	}
	result, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || !result.Ready || !result.StopConfirmed || result.Outcome != "ready" || result.Model != "target-current" || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
	for i := 0; i < 2; i++ {
		replayed, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
		if err != nil || replayed.Outcome != result.Outcome || calls.Load() != 1 {
			t.Fatal("original readiness replay launched another Host", replayed, err)
		}
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restarted.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || replayed.Outcome != "ready" || !replayed.StopConfirmed || calls.Load() != 1 {
		t.Fatal("restart lost original readiness receipt", replayed, err)
	}
	changed := request
	changed.Model = "different-public-model"
	if _, err := s.CheckOwnedRuntimeReadiness(t.Context(), changed); err == nil || calls.Load() != 1 {
		t.Fatal("original readiness intent changed")
	}
	read, err := restarted.ReadOwnedRuntimeReadiness(t.Context(), request.NodeID, request.Backend, request.OperationID)
	if err != nil || !read.Ready || !read.StopConfirmed {
		t.Fatal("original receipt lookup failed", read, err)
	}
}

func TestOwnedReadinessUnknownOutcomeBlocksReplayAndNewOperationIDs(t *testing.T) {
	s, request, calls := readinessFixture(t)
	s.readinessCheck = func(context.Context, caelis.OwnedHostOptions, string) (caelis.OwnedReadiness, error) {
		calls.Add(1)
		return caelis.OwnedReadiness{}, errors.New("synthetic cleanup outcome unconfirmed")
	}
	result, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || result.Outcome != "unknown" || result.Ready || result.StopConfirmed || calls.Load() != 1 {
		t.Fatal("unknown cleanup was called ready", result, err)
	}
	for _, op := range []string{request.OperationID, "different-enable-operation"} {
		attempt := request
		attempt.OperationID = op
		replayed, err := s.CheckOwnedRuntimeReadiness(t.Context(), attempt)
		if err != nil || replayed.Outcome != "unknown" || calls.Load() != 1 {
			t.Fatal("unknown operation launched another Host", replayed, err, calls.Load())
		}
	}
	read, err := s.ReadOwnedRuntimeReadiness(t.Context(), request.NodeID, request.Backend, "missing-original-operation")
	if err != nil || read.Outcome != "unknown" || calls.Load() != 1 {
		t.Fatal("missing receipt invented execution", read, err)
	}
	if _, err := os.Stat(s.readinessDirectory(request.Backend, "missing-original-operation")); !os.IsNotExist(err) {
		t.Fatal("receipt lookup created original intent", err)
	}
}

func TestOwnedReadinessPreservesAdmittedCheckAcrossObserverCancellation(t *testing.T) {
	s, request, calls := readinessFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	original := s.readinessCheck
	s.readinessCheck = func(life context.Context, opts caelis.OwnedHostOptions, model string) (caelis.OwnedReadiness, error) {
		cancel()
		if life.Err() != nil {
			t.Error("observer cancellation abandoned admitted readiness cleanup")
		}
		return original(life, opts, model)
	}
	result, err := s.CheckOwnedRuntimeReadiness(ctx, request)
	if err != nil || !result.Ready || !result.StopConfirmed || calls.Load() != 1 {
		t.Fatal("admitted action lost confirmed shutdown receipt", result, err)
	}
}

func TestOwnedReadinessFrozenBindingsAndCompanionAreRequired(t *testing.T) {
	for _, name := range []string{"node", "backend", "binary", "store", "model", "companion", "missing-companion"} {
		t.Run(name, func(t *testing.T) {
			s, request, calls := readinessFixture(t)
			switch name {
			case "node":
				request.NodeID = "foreign-node"
			case "backend":
				request.Backend = api.NodeCodex
			case "binary":
				request.ExpectedBinary = filepath.Join(s.options.Directory, "different-binary")
			case "store":
				request.ExpectedStore = filepath.Join(s.options.Directory, "different-store")
			case "model":
				request.Model = "model\nmutation"
			case "companion":
				s.options.OwnedRuntimeCompanion = func(context.Context) (OwnedRuntimeCompanion, error) {
					return OwnedRuntimeCompanion{Path: request.ExpectedBinary, SHA256: hex.EncodeToString(make([]byte, 32))}, nil
				}
			case "missing-companion":
				s.options.OwnedRuntimeCompanion = nil
			}
			if _, err := s.CheckOwnedRuntimeReadiness(t.Context(), request); err == nil || calls.Load() != 0 {
				t.Fatal("unreviewed binding reached Host readiness")
			}
			if _, err := os.Stat(filepath.Join(s.options.Directory, "owned-readiness")); !os.IsNotExist(err) {
				t.Fatal("invalid request created native intent", err)
			}
		})
	}
}

func TestOwnedReadinessClosedFramedActionAndReceiptLookup(t *testing.T) {
	s, request, calls := readinessFixture(t)
	body, _ := json.Marshal(request)
	body = append(body[:len(body)-1], []byte(`,"watchdogHelper":"/arbitrary"}`)...)
	response := httptest.NewRecorder()
	Handler(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/node/check-owned-readiness", bytes.NewReader(body)))
	if response.Code == http.StatusOK || calls.Load() != 0 {
		t.Fatal("payload helper became authority")
	}
	local, remote := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- productrpc.ServeNativeStream(t.Context(), remote, remote, Handler(s), allowed) }()
	c, err := NewClient(request.NodeID, local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); <-done }()
	result, err := c.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || !result.Ready || !result.StopConfirmed || calls.Load() != 1 {
		t.Fatal("explicit readiness IPC failed", result, err)
	}
	read, err := c.ReadOwnedRuntimeReadiness(t.Context(), request.NodeID, request.Backend, request.OperationID)
	if err != nil || !read.Ready || calls.Load() != 1 {
		t.Fatal("receipt query restarted Host", read, err)
	}
	if _, err := c.ReadOwnedRuntimeReadiness(t.Context(), "foreign", request.Backend, request.OperationID); err == nil {
		t.Fatal("receipt identity repinned")
	}
}
