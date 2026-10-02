package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSelectedWorkerFlagsKeepExactUppercaseEnrollmentIdentity(t *testing.T) {
	const id = "node-U2LZFVZMQOXHDBB4V3BEVGPWKI"
	var selected selectedWorkers
	for _, driver := range []string{"codex", "caelis"} {
		if err := selected.Set(id + "/" + driver); err != nil {
			t.Fatal("production-shaped enrolled ID rejected", err)
		}
	}
	root := t.TempDir()
	document := struct {
		Version int                        `json:"version"`
		Nodes   []backend.WorkerNodeConfig `json:"nodes"`
	}{Version: 1}
	for _, target := range selected {
		if target.NodeID != id || target.Role != api.RoleWorker {
			t.Fatal("selection normalized enrolled identity", target)
		}
		document.Nodes = append(document.Nodes, backend.WorkerNodeConfig{ID: id, Label: "Enrolled Worker", Backend: target.Backend, Transport: "registered-agent"})
	}
	b, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worker-nodes.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.ValidateConfiguredWorkerTargets(root, selected); err != nil {
		t.Fatal("selected native identity disagrees with saved Worker", err)
	}
	var lower selectedWorkers
	if err := lower.Set(strings.ToLower(id) + "/codex"); err != nil {
		t.Fatal(err)
	}
	if app.ValidateConfiguredWorkerTargets(root, lower) == nil {
		t.Fatal("case-folded flag selected original enrolled Worker")
	}
	if selected.Set(id+"/codex") == nil {
		t.Fatal("duplicate original selection accepted")
	}
	for _, value := range []string{"node/ESCAPE/codex", "node..ESCAPE/codex", "node_ESCAPE/codex", "-node/codex", "local/codex", strings.Repeat("A", 65) + "/codex"} {
		var bad selectedWorkers
		if bad.Set(value) == nil {
			t.Fatal("unsafe or reserved selection accepted", value)
		}
	}
}

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
