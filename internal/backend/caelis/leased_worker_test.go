//go:build darwin || linux

package caelis

import (
	"context"
	"errors"
	"net"
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
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
	"github.com/caelis-labs/caelis-bot/internal/workerlease"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
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
func ownedLeasedWorkerFixture(t *testing.T, providers ...api.WorkSourceProvider) (*LeasedWorker, *ownedWorkerLeaseReader, api.WorkDispatchSource, string) {
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
	var provider api.WorkSourceProvider = fixtureWorkSource{source}
	if len(providers) > 0 {
		provider = providers[0]
	}
	w, err := NewLeasedWorker(t.Context(), WorkerOptions{Directory: filepath.Join(dir, "worker-app"), Target: api.WorkTarget{NodeID: "worker-node", Backend: "caelis", Role: api.RoleWorker}, Source: provider}, OwnedHostOptions{NodeID: "worker-node", Binary: binary, Store: store, WatchdogHelper: helper}, workerlease.Options{BotID: source.Lease.BotID, BrokerNodeID: source.Lease.BrokerNodeID, SourceNode: source.NodeID, SourceBackend: source.Backend, Reader: reader})
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

type leasedWireOwner struct {
	*LeasedWorker
	pair workerwire.Pair
}

func (w leasedWireOwner) WorkerPair() workerwire.Pair { return w.pair }

func TestOwnedLeasedWorkerProductionOwnerStartsBeforeDispatchSource(t *testing.T) {
	w, reader, source, store := ownedLeasedWorkerFixture(t, workerwire.SourceProvider())
	pair := workerwire.Pair{Target: w.engine.workerTarget, BotID: api.ProfileBotID(source.Lease.BotID), SourceNode: source.NodeID, SourceBackend: source.Backend}
	owner := nodeworker.New(leasedWireOwner{LeasedWorker: w, pair: pair})
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal("source-less native owner setup failed", err)
	}
	if snapshot := w.Snapshot(); snapshot.Connection != "ready" {
		t.Fatal("prepared native metadata not ready", snapshot.Connection)
	}
	assertOwnedWorkerNotEnrolled(t, w, store)
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	serverStream, clientStream := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, serverStream) }()
	client, err := workerwire.NewClient(t.Context(), pair, fixtureWorkSource{source}, clientStream)
	if err != nil {
		cancel()
		_ = clientStream.Close()
		t.Fatal("read-only hello before source failed", err)
	}
	t.Cleanup(func() {
		client.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("worker observer did not detach")
		}
	})
	if !client.Ready() {
		t.Fatal("wire hello did not expose prepared native metadata")
	}
	assertOwnedWorkerNotEnrolled(t, w, store)
	// Admission confirms the real framed grant and model metadata, but does not
	// enroll or invent an activation before the original task intent is sent.
	if err := client.WorkAdmission(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertOwnedWorkerNotEnrolled(t, w, store)
	target := pair.Target
	in := api.WorkStart{ID: "first-wire-task", Workspace: filepath.Join(filepath.Dir(store), "workspace"), Source: source, Instructions: "bounded wire fixture", RequestDigest: digest([]byte("first wire intent")), TaskStart: api.TaskStart{RequestID: "original-wire-request", Title: "wire task", Prompt: "fixture prompt", Target: &target}}
	if _, err := client.StartWork(t.Context(), in); err != nil {
		t.Fatal("first exact framed mutation did not lazily enroll", err)
	}
	for _, name := range []string{"worker-enrolled", "worker-created", "worker-prompted"} {
		if _, err := os.Stat(filepath.Join(store, name)); err != nil {
			t.Fatal("actual owned worker effect missing", name, err)
		}
	}
	w.engine.mu.Lock()
	saved := w.engine.state.Workers[in.ID]
	w.engine.mu.Unlock()
	if saved.Source != source || saved.StartRequestID != in.RequestID {
		t.Fatal("lazy enrollment changed original source/request")
	}
	// Wait for the actual native state observer, then continue through a real
	// framed Source; the continuation keeps its exact original native request.
	observation, stopObservation := context.WithTimeout(t.Context(), 3*time.Second)
	defer stopObservation()
	for {
		w.engine.mu.Lock()
		observed := w.engine.state.Views[saved.Binding.SessionId] != nil
		revision := w.engine.revision
		w.engine.mu.Unlock()
		if observed {
			break
		}
		if _, err := w.WaitSnapshot(observation, revision); err != nil {
			t.Fatal("native Worker state did not arrive", err)
		}
	}
	message := api.TaskMessage{ID: in.ID, RequestID: "original-wire-continuation", Prompt: "continue fixture", Source: source, RequestDigest: digest([]byte("exact wire continuation"))}
	if _, err := client.SendWork(t.Context(), message); err != nil {
		t.Fatal("exact framed continuation failed", err)
	}
	w.engine.mu.Lock()
	retained := w.engine.state.Workers[in.ID].Messages[message.RequestID]
	w.engine.mu.Unlock()
	if retained.Source != source || retained.RequestDigest != message.RequestDigest {
		t.Fatal("lazy continuation changed original source/digest")
	}
	// A second new task is suspended in the actual broker read. Revocation
	// closes the same native ticket and cannot be turned into reenrollment.
	reader.mu.Lock()
	reader.block = make(chan struct{})
	reader.entered = make(chan struct{}, 1)
	entered := reader.entered
	reader.mu.Unlock()
	second := in
	second.ID = "queued-wire-task"
	second.RequestID = "queued-wire-request"
	result := make(chan error, 1)
	go func() { _, err := client.StartWork(t.Context(), second); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("queued wire task did not reach broker")
	}
	w.Revoke()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revoked queued wire mutation accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoked wire mutation did not return")
	}
	w.engine.mu.Lock()
	_, exists := w.engine.state.Workers[second.ID]
	w.engine.mu.Unlock()
	if exists {
		t.Fatal("lease-revoked wire mutation created an intent")
	}
	// The original task receipt remains readable after lease loss, without
	// obtaining a fresh Source or retrying enrollment/native task creation.
	if task, err := client.StartWork(t.Context(), in); err != nil || task.ID != in.ID {
		t.Fatal("original receipt could not reconcile after revocation", task.ID, err)
	}
	assertOwnedWorkerTreeStopped(t, store)
}
func assertOwnedWorkerNotEnrolled(t *testing.T, w *LeasedWorker, store string) {
	t.Helper()
	for _, path := range []string{workerSecretPath(w.engine.path), w.engine.path, filepath.Join(store, "runtime", "bot-worker-enrollments"), filepath.Join(store, "worker-enrolled"), filepath.Join(store, "worker-created"), filepath.Join(store, "worker-prompted")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read-only Worker startup wrote enrollment or task state", path, err)
		}
	}
}

type leasedControlSource struct {
	mu     sync.Mutex
	source api.WorkDispatchSource
	err    error
}

func (s *leasedControlSource) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source, s.err
}
func leasedControlWireFixture(t *testing.T, mode string) (*LeasedWorker, *ownedWorkerLeaseReader, *leasedControlSource, *workerwire.Client, string, string) {
	t.Helper()
	w, reader, source, store := ownedLeasedWorkerFixture(t, workerwire.SourceProvider())
	pair := workerwire.Pair{Target: w.engine.workerTarget, BotID: api.ProfileBotID(source.Lease.BotID), SourceNode: source.NodeID, SourceBackend: source.Backend}
	owner := nodeworker.New(leasedWireOwner{LeasedWorker: w, pair: pair})
	t.Cleanup(func() {
		if err := owner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := owner.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	server, err := workerwire.NewServer(owner, pair)
	if err != nil {
		t.Fatal(err)
	}
	serverStream, clientStream := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, serverStream) }()
	nativeSource := &leasedControlSource{source: source}
	client, err := workerwire.NewClient(t.Context(), pair, nativeSource, clientStream)
	if err != nil {
		cancel()
		_ = clientStream.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("control observer did not detach")
		}
	})
	if err := os.WriteFile(filepath.Join(store, "fixture-control-mode"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	in := api.WorkStart{ID: "controlled-task", Workspace: filepath.Join(filepath.Dir(store), "workspace"), Source: source, RequestDigest: digest([]byte("control fixture intent")), TaskStart: api.TaskStart{RequestID: "controlled-request", Title: "controlled task", Prompt: "fixture task", Target: &pair.Target}}
	if _, err := client.StartWork(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	observation, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	for {
		w.engine.mu.Lock()
		v := w.engine.state.Views["owned-worker-session"]
		observed := v != nil && (!strings.Contains(mode, "decision") || v.State.Approval.Active != nil)
		revision := w.engine.revision
		w.engine.mu.Unlock()
		if observed {
			break
		}
		if _, err := w.WaitSnapshot(observation, revision); err != nil {
			t.Fatal("control native head missing", err)
		}
	}
	nativeSource.mu.Lock()
	nativeSource.source.OperationID = "actual-control-request"
	nativeSource.mu.Unlock()
	return w, reader, nativeSource, client, in.ID, store
}
func ownedControlCount(t *testing.T, store, kind string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store, "worker-"+kind+"-count"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return count
}
func TestOwnedLeasedWorkerWireControlsUseExactCurrentOrIdleOriginalLeaseAndNeverReplayUnknown(t *testing.T) {
	for _, kind := range []string{"stop", "decision"} {
		for _, unknown := range []bool{false, true} {
			mode := kind
			if unknown {
				mode = "unknown-" + kind
			}
			t.Run(mode, func(t *testing.T) {
				w, _, source, client, id, store := leasedControlWireFixture(t, mode)
				effectKind := "cancel"
				if kind == "decision" {
					effectKind = "decision"
				}
				var approval api.WorkApproval
				if kind == "decision" {
					approvals := w.WorkApprovals()
					if len(approvals) != 1 {
						t.Fatal("native approval not projected", len(approvals))
					}
					approval = approvals[0]
				}
				control := func() error {
					if kind == "stop" {
						_, err := client.StopWork(t.Context(), id)
						return err
					}
					return client.DecideWork(t.Context(), approval, api.Decision{ID: approval.Approval.ID, Choice: "native-allow"})
				}
				source.mu.Lock()
				source.err = errors.New("native callback fault")
				source.mu.Unlock()
				if err := control(); err == nil {
					t.Fatal("source fault hidden")
				}
				if count := ownedControlCount(t, store, effectKind); count != 0 {
					t.Fatal("source fault sent native bytes", count)
				}
				source.mu.Lock()
				source.err = api.ErrWorkSourceInactive
				source.mu.Unlock()
				if kind == "stop" {
					if _, err := w.StopWork(t.Context(), id); err == nil {
						t.Fatal("unpaired missing current source allowed cancel")
					}
				} else {
					if err := w.DecideWork(t.Context(), approval, api.Decision{ID: approval.Approval.ID, Choice: "native-allow"}); err == nil {
						t.Fatal("unpaired missing current source allowed approval")
					}
				}
				if count := ownedControlCount(t, store, effectKind); count != 0 {
					t.Fatal("missing source sent native control bytes", count)
				}
				idleErr := control()
				if unknown && idleErr == nil || !unknown && idleErr != nil {
					t.Fatal("idle paired original-task control outcome changed", unknown, idleErr)
				}
				if count := ownedControlCount(t, store, effectKind); count != 1 {
					t.Fatal("idle UI control did not use original lease exactly once", count)
				}
				source.mu.Lock()
				source.err = nil
				originalGrant := source.source.Lease
				source.source.Lease = api.WorkerLeaseGrant{}
				source.mu.Unlock()
				if err := control(); err == nil {
					t.Fatal("missing current lease allowed control")
				}
				if count := ownedControlCount(t, store, effectKind); count != 1 {
					t.Fatal("missing lease sent additional native control bytes", count)
				}
				source.mu.Lock()
				source.source.Lease = originalGrant
				source.mu.Unlock()
				err := control()
				if unknown && err == nil || !unknown && err != nil {
					t.Fatal("native control outcome changed", unknown, err)
				}
				if count := ownedControlCount(t, store, effectKind); count != 1 {
					t.Fatal("valid exact control was not dispatched once", count)
				}
				if unknown {
					if kind == "stop" {
						if err := os.WriteFile(filepath.Join(store, "fixture-control-mode"), []byte("unknown-stop-new-turn"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					if err := control(); err == nil {
						t.Fatal("unknown original control became accepted")
					}
					if count := ownedControlCount(t, store, effectKind); count != 1 {
						t.Fatal("unknown original control reissued", count)
					}
					source.mu.Lock()
					source.source.Lease.Epoch = "replacement-epoch"
					source.mu.Unlock()
					if err := control(); err == nil {
						t.Fatal("new lease epoch acted on old unknown control")
					}
					if count := ownedControlCount(t, store, effectKind); count != 1 {
						t.Fatal("new epoch replayed old control", count)
					}
				}
				w.Revoke()
				assertOwnedWorkerTreeStopped(t, store)
			})
		}
	}
}
func TestOwnedLeasedWorkerQueuedWireControlsCannotPassRevocation(t *testing.T) {
	for _, kind := range []string{"stop", "decision"} {
		t.Run(kind, func(t *testing.T) {
			w, reader, _, client, id, store := leasedControlWireFixture(t, kind)
			var approval api.WorkApproval
			if kind == "decision" {
				approvals := w.WorkApprovals()
				if len(approvals) != 1 {
					t.Fatal("native approval missing")
				}
				approval = approvals[0]
			}
			reader.mu.Lock()
			reader.block = make(chan struct{})
			reader.entered = make(chan struct{}, 1)
			entered := reader.entered
			reader.mu.Unlock()
			done := make(chan error, 1)
			go func() {
				if kind == "stop" {
					_, err := client.StopWork(t.Context(), id)
					done <- err
				} else {
					done <- client.DecideWork(t.Context(), approval, api.Decision{ID: approval.Approval.ID, Choice: "native-allow"})
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("control did not reach actual broker fence")
			}
			w.Revoke()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("revoked queued control accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("revoked control did not return")
			}
			effectKind := "cancel"
			if kind == "decision" {
				effectKind = "decision"
			}
			if count := ownedControlCount(t, store, effectKind); count != 0 {
				t.Fatal("revoked queued control sent native bytes", count)
			}
			assertOwnedWorkerTreeStopped(t, store)
		})
	}
}

func TestOwnedLeasedWorkerIdlePairedControlsStillRequireLiveOriginalBroker(t *testing.T) {
	for _, kind := range []string{"stop", "decision"} {
		t.Run(kind, func(t *testing.T) {
			w, reader, source, client, id, store := leasedControlWireFixture(t, kind)
			source.mu.Lock()
			source.err = api.ErrWorkSourceInactive
			source.mu.Unlock()
			reader.mu.Lock()
			reader.err = errors.New("original paired broker lease revoked")
			reader.mu.Unlock()
			if kind == "stop" {
				if _, err := client.StopWork(t.Context(), id); err == nil {
					t.Fatal("idle cancel ignored original broker revocation")
				}
			} else {
				approvals := w.WorkApprovals()
				if len(approvals) != 1 {
					t.Fatal("approval missing")
				}
				a := approvals[0]
				if err := client.DecideWork(t.Context(), a, api.Decision{ID: a.Approval.ID, Choice: "native-allow"}); err == nil {
					t.Fatal("idle approval ignored original broker revocation")
				}
			}
			effectKind := "cancel"
			if kind == "decision" {
				effectKind = "decision"
			}
			if count := ownedControlCount(t, store, effectKind); count != 0 {
				t.Fatal("expired idle control sent native bytes", count)
			}
			assertOwnedWorkerTreeStopped(t, store)
		})
	}
}
