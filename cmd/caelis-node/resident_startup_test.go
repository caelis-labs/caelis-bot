package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type startupFixture struct {
	snapshot                             backend.WorkerNodeSetup
	calls                                []api.WorkTarget
	revisions                            []uint64
	snapshots                            int
	failAt                               int
	failure, cleanup                     error
	candidate                            bool
	prepared, started, published, closed bool
	detached                             []api.WorkTarget
	onStart                              func()
	closeCalls                           int
}

func startupFixtureFor() *startupFixture {
	return &startupFixture{snapshot: backend.WorkerNodeSetup{Revision: 7, Nodes: []backend.WorkerNodeView{{Config: backend.WorkerNodeConfig{ID: "same-node", Backend: "caelis"}}, {Config: backend.WorkerNodeConfig{ID: "same-node", Backend: "codex"}}}}}
}
func (s *startupFixture) WorkerNodes() backend.WorkerNodeSetup { s.snapshots++; return s.snapshot }
func (s *startupFixture) ConnectWorkerTarget(_ context.Context, target api.WorkTarget, revision uint64) (backend.WorkerNodeSetup, error) {
	if !s.prepared || s.started || s.published {
		panic("connection outside explicit startup")
	}
	s.calls = append(s.calls, target)
	s.revisions = append(s.revisions, revision)
	if s.failAt == len(s.calls) {
		return s.snapshot, s.failure
	}
	for i := range s.snapshot.Nodes {
		if startupTarget(s.snapshot.Nodes[i].Config) == target {
			s.snapshot.Nodes[i].Connected = true
			s.snapshot.Nodes[i].State = "ready"
			if s.candidate {
				s.snapshot.Nodes[i].State = "candidate"
				s.snapshot.Nodes[i].Issue = "model_setup_required"
			}
		}
	}
	s.snapshot.Revision++
	return s.snapshot, nil
}
func (s *startupFixture) PreparePersonal() error { s.prepared = true; return nil }
func (s *startupFixture) Start() error {
	s.started = true
	if s.onStart != nil {
		s.onStart()
	}
	return nil
}
func (s *startupFixture) Close() error {
	s.closeCalls++
	if !s.closed {
		for _, v := range s.snapshot.Nodes {
			if v.Connected {
				s.detached = append(s.detached, startupTarget(v.Config))
			}
		}
	}
	s.closed = true
	return s.cleanup
}
func (s *startupFixture) setup() (func() error, error) {
	return func() error {
		if !s.started {
			panic("ready before Start")
		}
		s.published = true
		return nil
	}, nil
}
func startupSelections() []api.WorkTarget {
	return []api.WorkTarget{{NodeID: "same-node", Backend: "caelis", Role: api.RoleWorker}, {NodeID: "same-node", Backend: "codex", Role: api.RoleWorker}}
}

func TestResidentStartupDefaultDoesNotInspectOrConnectWorkers(t *testing.T) {
	s := startupFixtureFor()
	s.snapshot.Issue = "config_unreadable"
	if err := runResidentOwner(t.Context(), s, s, nil, s.setup); err != nil {
		t.Fatal(err)
	}
	if s.snapshots != 0 || len(s.calls) != 0 || !s.started || !s.published || !s.closed {
		t.Fatal("default startup acquired Worker prerequisite", s)
	}
}
func TestResidentStartupSameMachineExactBackendsBeforeReady(t *testing.T) {
	s := startupFixtureFor()
	selected := startupSelections()
	if err := runResidentOwner(t.Context(), s, s, selected, s.setup); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.calls, selected) || !reflect.DeepEqual(s.revisions, []uint64{7, 8}) || !s.published || !reflect.DeepEqual(s.detached, selected) {
		t.Fatal("wrong backend, stale revision or ownership", s)
	}
}
func TestResidentStartupRejectsWholeInvalidSelectionBeforeConnection(t *testing.T) {
	valid := startupSelections()[0]
	for _, selected := range [][]api.WorkTarget{{valid, valid}, {valid, {NodeID: "unknown", Backend: "caelis", Role: api.RoleWorker}}, {valid, {NodeID: "same-node", Backend: "caelis", Role: api.RoleBot}}} {
		s := startupFixtureFor()
		if err := runResidentOwner(t.Context(), s, s, selected, s.setup); err == nil {
			t.Fatal("invalid startup accepted")
		}
		if len(s.calls) != 0 || s.started || s.published || !s.closed {
			t.Fatal("invalid later selection had effects", s)
		}
	}
}
func TestResidentStartupFailureDetachesAndPreservesErrorChain(t *testing.T) {
	failure := errors.New("native connection failed")
	cleanup := errors.New("owned detach unconfirmed")
	s := startupFixtureFor()
	s.failAt = 2
	s.failure = failure
	s.cleanup = cleanup
	err := runResidentOwner(t.Context(), s, s, startupSelections(), s.setup)
	if !errors.Is(err, failure) || !errors.Is(err, cleanup) || s.started || s.published || !s.closed || !reflect.DeepEqual(s.detached, startupSelections()[:1]) {
		t.Fatal("startup/cleanup uncertainty hidden or ready published", err, s)
	}
}
func TestResidentStartupCandidateAndCancellationNeverPublishReady(t *testing.T) {
	s := startupFixtureFor()
	s.candidate = true
	if err := runResidentOwner(t.Context(), s, s, startupSelections()[:1], s.setup); err == nil || s.started || s.published || len(s.detached) != 1 {
		t.Fatal("candidate advertised ready", err, s)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s = startupFixtureFor()
	if err := runResidentOwner(ctx, s, s, startupSelections(), s.setup); !errors.Is(err, context.Canceled) || len(s.calls) != 0 || s.started || s.published || !s.closed {
		t.Fatal("canceled startup effects", err, s)
	}
	s = startupFixtureFor()
	s.failAt = 1
	s.failure = context.DeadlineExceeded
	if err := runResidentOwner(t.Context(), s, s, startupSelections(), s.setup); !errors.Is(err, context.DeadlineExceeded) || s.published || !s.closed {
		t.Fatal("connect cancellation cause lost", err, s)
	}
}

func TestResidentStartupCanceledDuringStartDoesNotPublishReady(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := startupFixtureFor()
	s.onStart = cancel
	s.cleanup = errors.New("owned detach unconfirmed")
	err := runResidentOwner(ctx, s, s, startupSelections(), s.setup)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, s.cleanup) || !s.started || s.published || s.closeCalls != 1 || !reflect.DeepEqual(s.detached, startupSelections()) {
		t.Fatal("canceled Start published readiness or hid cleanup", err, s)
	}
}
func TestConnectWorkerFlagsAndUnknownConfigRejectBeforeProfileEffects(t *testing.T) {
	parent := t.TempDir()
	profile := filepath.Join(parent, "not-created")
	for _, flags := range [][]string{{"--connect-worker", "same-node"}, {"--connect-worker", "same-node/caelis", "--connect-worker", "same-node/caelis"}, {"--connect-worker", "same-node/other"}, {"--connect-worker", "same-node/caelis"}} {
		args := append([]string{"serve-bot", "--profile", profile, "--auth-file", filepath.Join(parent, "not-read")}, flags...)
		var output bytes.Buffer
		if err := run(t.Context(), args, &output); err == nil {
			t.Fatal("invalid config accepted")
		}
		if _, err := os.Stat(profile); !os.IsNotExist(err) {
			t.Fatal("profile written before preflight", err)
		}
	}
	var selected selectedWorkers
	if selected.Set("same-node/caelis") != nil || selected.Set("same-node/codex") != nil || len(selected) != 2 {
		t.Fatal("exact same-node backends rejected")
	}
}
