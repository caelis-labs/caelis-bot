//go:build (darwin && cgo) || linux

package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestOwnedReadinessProductionDefaultCompanionStopsActualForegroundWithoutModelCall(t *testing.T) {
	s, request, _ := readinessFixture(t)
	directory := t.TempDir()
	testBinary := filepath.Join(directory, "caelis-public-fixture")
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "test", "-c", "-o", testBinary, "./internal/backend/caelis")
	build.Dir = filepath.Clean(filepath.Join(packageDir, "..", ".."))
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal("public foreground fixture build failed", err, string(output))
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(s.options.Directory, "caelis")
	helper := filepath.Join(s.options.Directory, "caelis-node")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(testBinary)+" -test.run='^TestOwnedReadinessProcessHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(testBinary)+" -test.run='^TestOwnedCaelisWatchdogHelper$' -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	request.ExpectedBinary = binary
	s.readinessCheck = nil
	s.options.OwnedRuntimeCompanion = nil // Fixed paired native companion discovery.
	s.options.OwnedRuntimeSettings = func(context.Context, api.NodeBackend) (OwnedRuntimeSettings, error) {
		return OwnedRuntimeSettings{Backend: request.Backend, Binary: binary, Store: request.ExpectedStore}, nil
	}
	metadata, err := ReadNativeCompanionMetadata(s.options.Directory, request.NodeID)
	if err != nil || metadata.Helper != helper || metadata.HelperSHA256 == "" {
		t.Fatal("actual companion not selected", metadata, err)
	}
	result, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || !result.Ready || !result.StopConfirmed || result.Model != "native-owned-current" || result.NativeConfigRevision != "42" {
		t.Fatal("production readiness action failed", result, err)
	}
	pids, err := os.ReadFile(filepath.Join(request.ExpectedStore, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(pids))) != 2 {
		t.Fatal("exact root/tool fixture identities missing")
	}
	for _, value := range strings.Fields(string(pids)) {
		pid, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("readiness published before native root/tool exit", pid, err)
		}
	}
	if _, err := os.Stat(filepath.Join(request.ExpectedStore, "runtime/service/discovery.json")); !os.IsNotExist(err) {
		t.Fatal("stopped native discovery survived", err)
	}
	requests, err := os.ReadFile(filepath.Join(request.ExpectedStore, "fixture-requests"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range strings.Split(strings.TrimSpace(string(requests)), "\n") {
		if value != "GET /initialize" && value != "GET /status" && value != "POST /completion/slash-arguments" {
			t.Fatal("readiness dispatched native/model mutation", value)
		}
	}
	replayed, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || !replayed.StopConfirmed || !replayed.Ready {
		t.Fatal("original production receipt failed", replayed, err)
	}
	after, err := os.ReadFile(filepath.Join(request.ExpectedStore, "fixture-requests"))
	if err != nil || !bytes.Equal(requests, after) {
		t.Fatal("original receipt replay started another foreground", err)
	}
	// A separately approved action can report unavailable only after stopping
	// its own foreground; this is distinct from an unknown cleanup outcome.
	if err := os.WriteFile(filepath.Join(request.ExpectedStore, "fixture-mode"), []byte("noauth"), 0600); err != nil {
		t.Fatal(err)
	}
	request.OperationID = "approved-noauth-readiness"
	unready, err := s.CheckOwnedRuntimeReadiness(t.Context(), request)
	if err != nil || unready.Ready || !unready.StopConfirmed || unready.Outcome != "unavailable" || unready.Reason != "owned-model-authentication-required" {
		t.Fatal("unavailable metadata lacked confirmed shutdown", unready, err)
	}
	pids, err = os.ReadFile(filepath.Join(request.ExpectedStore, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range strings.Fields(string(pids)) {
		pid, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("unready result preceded owned root/tool shutdown", pid, err)
		}
	}
	requests, err = os.ReadFile(filepath.Join(request.ExpectedStore, "fixture-requests"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range strings.Split(strings.TrimSpace(string(requests)), "\n") {
		if value != "GET /initialize" && value != "GET /status" && value != "POST /completion/slash-arguments" {
			t.Fatal("unready action dispatched native/model mutation", value)
		}
	}
}
