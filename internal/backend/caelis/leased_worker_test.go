//go:build darwin || linux

package caelis

import (
	"context"
	"errors"
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
	"github.com/caelis-labs/caelis-bot/internal/workerlease"
)

type ownedWorkerLeaseReader struct {
	mu             sync.Mutex
	err            error
	ttl            int64
	block, entered chan struct{}
}

func (r *ownedWorkerLeaseReader) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	r.mu.Lock()
	err, ttl, block, entered := r.err, r.ttl, r.block, r.entered
	r.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return nodeplane.Lease{}, ctx.Err()
		case <-block:
		}
	}
	if err != nil {
		return nodeplane.Lease{}, err
	}
	return nodeplane.Lease{BotID: ref.BotID, NodeID: ref.SourceNode, Backend: ref.SourceBackend, Epoch: ref.Epoch, TTLMs: ttl, ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func ownedLeasedWorkerFixture(t *testing.T) (*LeasedWorker, *ownedWorkerLeaseReader, api.WorkDispatchSource, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "caelis-leased-worker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	binary, helper := filepath.Join(dir, "caelis"), filepath.Join(dir, "watchdog")
	for path, name := range map[string]string{binary: "TestOwnedCaelisProcessHelper", helper: "TestOwnedCaelisWatchdogHelper"} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^"+name+"$' -- \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	source := api.WorkDispatchSource{NodeID: "actual-primary", Backend: "codex", BindingID: "native-binding", OperationID: "native-request", Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: "raw-profile-bot", BrokerNodeID: "paired-broker", SourceNodeID: "actual-primary", Backend: "codex", Epoch: "original-epoch"}}
	reader := &ownedWorkerLeaseReader{ttl: 60000}
	store := filepath.Join(dir, "private-store")
	w, err := NewLeasedWorker(t.Context(), WorkerOptions{Directory: filepath.Join(dir, "worker-app"), Target: api.WorkTarget{NodeID: "worker-node", Backend: "caelis", Role: api.RoleWorker}, Source: fixtureWorkSource{source}}, OwnedHostOptions{NodeID: "worker-node", Binary: binary, Store: store, WatchdogHelper: helper}, workerlease.Options{BotID: source.Lease.BotID, BrokerNodeID: source.Lease.BrokerNodeID, SourceNode: source.NodeID, SourceBackend: source.Backend, Reader: reader})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return w, reader, source, store
}
func TestOwnedLeasedWorkerChecksLeaseBeforePublicNativeBytes(t *testing.T) {
	w, reader, source, store := ownedLeasedWorkerFixture(t)
	if !w.LeaseAwareAdmission() || NewWorker(WorkerOptions{}).LeaseAwareAdmission() {
		t.Fatal("ownership capability not tied to private Host")
	}
	ctx, release, err := w.fence.BeginSource(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	d, token, err := Discover(w.engine.owned.settings)
	if err != nil {
		t.Fatal(err)
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	c.nativeWorkerDispatch = w.fence
	var receipt wire.CommandResult
	if err := c.json(ctx, "POST", "/fixture/worker-effect", struct{}{}, &receipt, "first-operation", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store, "worker-effect")); err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	reader.block = make(chan struct{})
	reader.entered = make(chan struct{}, 1)
	entered := reader.entered
	reader.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		done <- c.json(ctx, "POST", "/fixture/worker-effect", struct{}{}, &receipt, "second-operation", "")
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach final broker fence")
	}
	w.Revoke()
	if err := <-done; err == nil {
		t.Fatal("revoked worker sent public native bytes")
	}
	if _, err := os.Stat(filepath.Join(store, "worker-effect")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("revoked worker dispatched native effect", err)
	}
	if w.engine.owned.process.Live() || w.LeaseAwareAdmission() {
		t.Fatal("revocation retained private Host lifetime")
	}
	assertOwnedWorkerTreeStopped(t, store)
}
func TestOwnedLeasedWorkerExpiresWithoutAnotherDispatch(t *testing.T) {
	w, reader, source, store := ownedLeasedWorkerFixture(t)
	reader.mu.Lock()
	reader.ttl = 16200
	reader.mu.Unlock()
	ctx, release, err := w.fence.BeginSource(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("lease expiry retained dispatch ticket")
	}
	if err := w.FenceStop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.engine.owned.process.Live() || w.LeaseAwareAdmission() {
		t.Fatal("lease expiry retained private Host")
	}
	assertOwnedWorkerTreeStopped(t, store)
}

func assertOwnedWorkerTreeStopped(t *testing.T, store string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("owned Worker root or retained child survived", pid, err)
		}
	}
}

func TestOwnedLeasedWorkerBoundedStartRetainsPrivateSource(t *testing.T) {
	w, _, source, store := ownedLeasedWorkerFixture(t)
	ctx, release, err := w.fence.BeginSource(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// Exercise the actual owned endpoint and application enrollment without
	// introducing unrelated observer streams into this native start fixture.
	if err := w.engine.connectWorker(ctx); err != nil {
		t.Fatal(err)
	}
	w.engine.mu.Lock()
	w.engine.connected = true
	w.engine.mu.Unlock()
	target := w.engine.workerTarget
	in := api.WorkStart{ID: "owned-task", Workspace: filepath.Join(filepath.Dir(store), "workspace"), Instructions: "bounded task fixture", Source: source, RequestDigest: digest([]byte("owned-task-intent")), TaskStart: api.TaskStart{RequestID: "owned-request", Title: "owned task", Prompt: "fixture prompt", Target: &target}}
	if _, err := w.StartWork(ctx, in); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"worker-created", "worker-prompted"} {
		if _, err := os.Stat(filepath.Join(store, name)); err != nil {
			t.Fatal("owned Worker native protocol did not complete", name, err)
		}
	}
	w.engine.mu.Lock()
	saved := w.engine.state.Workers[in.ID]
	w.engine.mu.Unlock()
	if saved.Native || saved.Source != source || saved.Binding.SessionId != "owned-worker-session" {
		t.Fatal("bounded Worker lost private lease provenance", saved.Native)
	}
	changed := in
	changed.ID = "foreign-task"
	changed.Source.Lease.BotID = "transport-hash"
	if _, err := w.StartWork(ctx, changed); err == nil {
		t.Fatal("public projection admitted a foreign private Source")
	}
	w.Revoke()
	assertOwnedWorkerTreeStopped(t, store)
}
