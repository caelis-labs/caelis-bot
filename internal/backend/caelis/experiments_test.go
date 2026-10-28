package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type experimentFixture struct {
	mu       sync.Mutex
	settings api.RuntimeSettings
	revision int
	values   wire.ExperimentalValues
	posts    int
	mode     string
	receipts map[string]wire.CommandResult
	block    func(http.ResponseWriter, *http.Request) bool
	features []string
}

func newExperimentFixture(t *testing.T) *experimentFixture {
	f := &experimentFixture{revision: 1, values: wire.ExperimentalValues{AsyncCompact: pointer(false), IdleCompact: pointer(false)}, receipts: map[string]wire.CommandResult{}, features: append([]string{}, experimentalIDs...)}
	f.settings = setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		block := f.block
		f.mu.Unlock()
		if block != nil && block(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case p == "/initialize":
			caps := append([]string{}, required...)
			if f.mode != "unsupported" {
				caps = append(caps, experimentalCapability)
			}
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("setup-instance"), Capabilities: caps})
		case p == "/sessions" && r.Method == "POST":
			var req wire.CreateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("fixture-session")})
		case p == "/sessions/fixture-session/profile":
			writeFixture(w, wire.SessionBinding{SessionId: "fixture-session", PrincipalId: "local-owner", Profile: wire.SessionProfile{Execution: "tools-only"}})
		case strings.HasPrefix(p, "/operations/"):
			op := strings.TrimPrefix(p, "/operations/")
			v, ok := f.receipts[op]
			if !ok {
				w.WriteHeader(404)
				return
			}
			writeFixture(w, v)
		case p == "/configuration/experimental" && r.Method == "GET":
			features := []wire.ExperimentalDefinition{}
			for _, id := range f.features {
				features = append(features, wire.ExperimentalDefinition{Id: id})
			}
			writeFixture(w, wire.ExperimentalSnapshot{Scope: "host", AppliesAt: "next_runtime_activation", Revision: wire.Uint64Decimal(strconv.Itoa(f.revision)), Desired: f.values, Features: features})
		case p == "/configuration/experimental" && r.Method == "POST":
			var req wire.ExperimentalRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			f.posts++
			if r.Header.Get("Idempotency-Key") != req.OperationId || r.Header.Get("If-Match") != `"`+string(req.ExpectedRevision)+`"` {
				t.Error("lost operation/revision")
			}
			outcome := "committed"
			if string(req.ExpectedRevision) != strconv.Itoa(f.revision) {
				outcome = "conflicted"
			} else if f.mode == "reject" {
				outcome = "rejected"
			} else {
				if req.Values.AsyncCompact != nil {
					f.values.AsyncCompact = req.Values.AsyncCompact
				}
				if req.Values.IdleCompact != nil {
					f.values.IdleCompact = req.Values.IdleCompact
				}
				f.revision++
			}
			result := wire.CommandResult{OperationId: req.OperationId, Outcome: wire.Outcome(outcome)}
			f.receipts[req.OperationId] = result
			if f.mode == "drop" {
				f.receipts[req.OperationId] = wire.CommandResult{OperationId: req.OperationId, Outcome: "unknown"}
				drop(w)
				return
			}
			if f.mode == "wrong" {
				f.receipts[req.OperationId] = wire.CommandResult{OperationId: "different-owner", Outcome: "committed"}
				result = f.receipts[req.OperationId]
			}
			writeFixture(w, result)
		default:
			t.Errorf("unexpected experimental path %s", p)
		}
	})
	return f
}
func connectFixtureExperiments(t *testing.T, f *experimentFixture, dir string) {
	t.Helper()
	s := New(Options{Directory: dir, Settings: f.settings, ToolsOnly: true, RequireApproval: true, Execution: api.ExecutionSettings{Model: "fixture/model"}})
	// Exercise the resident's actual connect/ensureSession path without starting
	// observation loops; no model work is submitted by this synthetic connection.
	s.retainedWorkers = true
	if err := s.connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
}
func readFixtureExperiments(t *testing.T, f *experimentFixture, dir string) api.RuntimeExperiments {
	t.Helper()
	v, err := ReadRuntimeExperiments(t.Context(), f.settings, dir)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func changeFixtureExperiment(t *testing.T, f *experimentFixture, dir, id string, on bool, rev string) api.RuntimeExperiments {
	t.Helper()
	v, err := ChangeRuntimeExperiments(t.Context(), f.settings, dir, api.RuntimeExperimentChange{ID: id, Enabled: on, ExpectedRevision: rev})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestExperimentsConnectionAndReadsNeverWriteDefaults(t *testing.T) {
	f := newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "profile-a")
	v := readFixtureExperiments(t, f, dir)
	connectFixtureExperiments(t, f, dir)
	for _, feature := range v.Features {
		if feature.Enabled {
			t.Fatal("Core default was changed", v)
		}
	}
	if f.count() != 0 {
		t.Fatal("first connection/read wrote experimental configuration")
	}
	// A makes explicit choices before B's first connection. Bootstrap receipts
	// are absent/expired, which must never authorize an implicit write.
	for _, id := range experimentalIDs {
		v = changeFixtureExperiment(t, f, dir, id, true, v.Revision)
	}
	v = changeFixtureExperiment(t, f, dir, "async_compact", false, v.Revision)
	before := f.count()
	f.mu.Lock()
	f.receipts = map[string]wire.CommandResult{}
	f.mu.Unlock()
	for _, profile := range []string{dir, filepath.Join(t.TempDir(), "profile-b")} {
		connectFixtureExperiments(t, f, profile)
		current := readFixtureExperiments(t, f, profile)
		if current.Features[0].Enabled || !current.Features[1].Enabled || f.count() != before {
			t.Fatal("connection/read overwrote user's choices", current)
		}
	}
	stale := changeFixtureExperiment(t, f, dir, "idle_compact", false, "1")
	if stale.SaveState != "conflicted" || f.count() != before {
		t.Fatal("stale tab wrote", stale)
	}
	v = changeFixtureExperiment(t, f, dir, "idle_compact", false, v.Revision)
	if v.Features[1].Enabled || v.Features[0].Enabled {
		t.Fatal("switches coupled", v)
	}
}
func TestExperimentsUnknownReadsOriginalReceiptWithoutReplay(t *testing.T) {
	for _, mode := range []string{"drop", "wrong"} {
		t.Run(mode, func(t *testing.T) {
			f := newExperimentFixture(t)
			dir := filepath.Join(t.TempDir(), "profile")
			f.setMode(mode)
			v := changeFixtureExperiment(t, f, dir, "async_compact", true, "1")
			if v.SaveState != "pending" {
				t.Fatal(v)
			}
			op := v.OperationID
			connectFixtureExperiments(t, f, dir)
			for range 2 {
				v = readFixtureExperiments(t, f, dir)
				if v.SaveState != "pending" {
					t.Fatal("wrong or unknown receipt settled", v)
				}
			}
			v = changeFixtureExperiment(t, f, dir, "idle_compact", true, v.Revision)
			if f.count() != 1 || v.OperationID != op {
				t.Fatal("unknown write replayed", v)
			}
			f.mu.Lock()
			f.receipts[op] = wire.CommandResult{OperationId: op, Outcome: "committed"}
			f.mode = ""
			f.mu.Unlock()
			v = readFixtureExperiments(t, f, dir)
			if v.SaveState != "saved" || !v.Features[0].Enabled || f.count() != 1 {
				t.Fatal(v)
			}
			v = changeFixtureExperiment(t, f, dir, "idle_compact", true, v.Revision)
			if v.SaveState != "saved" || v.OperationID == op || f.count() != 2 {
				t.Fatal("new explicit action did not get new ID", v)
			}
		})
	}
}
func TestExperimentsRejectionUnsupportedAndSuperseded(t *testing.T) {
	f := newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "profile")
	f.setMode("unsupported")
	connectFixtureExperiments(t, f, dir)
	v := readFixtureExperiments(t, f, dir)
	if v.Supported || f.count() != 0 {
		t.Fatal(v)
	}
	f.setMode("reject")
	v = changeFixtureExperiment(t, f, dir, "async_compact", true, "1")
	if v.SaveState != "rejected" {
		t.Fatal(v)
	}
	old := v.OperationID
	f.setMode("")
	v = changeFixtureExperiment(t, f, dir, "async_compact", true, v.Revision)
	if v.OperationID == old || v.SaveState != "saved" {
		t.Fatal(v)
	}
	f.mu.Lock()
	f.values.AsyncCompact = pointer(false)
	f.revision++
	f.mu.Unlock()
	v = readFixtureExperiments(t, f, dir)
	if v.SaveState != "superseded" || v.Features[0].Enabled {
		t.Fatal("current read rolled back to historical receipt", v)
	}
}

func (f *experimentFixture) count() int          { f.mu.Lock(); defer f.mu.Unlock(); return f.posts }
func (f *experimentFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }

func TestExperimentsBlockedHostDoesNotBlockAnotherHostOrLocalJournal(t *testing.T) {
	slow, fast := newExperimentFixture(t), newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "slow")
	entered, release := make(chan struct{}), make(chan struct{})
	slow.mu.Lock()
	slow.block = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/configuration/experimental") {
			return false
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		return true // Deliberately leave the original result unknown.
	}
	slow.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ChangeRuntimeExperiments(ctx, slow.settings, dir, api.RuntimeExperimentChange{ID: "async_compact", Enabled: true, ExpectedRevision: "1"})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow request did not dispatch")
	}
	defer close(release)
	quick, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	c, info, err := experimentalClient(quick, slow.settings)
	if err != nil {
		t.Fatal(err)
	}
	c.http.CloseIdleConnections()
	// Even this same Store's journal is unlocked while its POST is blocked.
	unlock, err := lockExperimentJournal(quick, experimentPath(dir, info))
	if err != nil {
		t.Fatal("network request held journal lock", err)
	}
	unlock()
	v, err := ChangeRuntimeExperiments(quick, fast.settings, filepath.Join(t.TempDir(), "fast"), api.RuntimeExperimentChange{ID: "idle_compact", Enabled: true, ExpectedRevision: "1"})
	if err != nil || v.SaveState != "saved" {
		t.Fatal("different Host blocked", err, v)
	}
	// Another request to the slow Host observes pending instead of duplicating it.
	v, err = ChangeRuntimeExperiments(quick, slow.settings, dir, api.RuntimeExperimentChange{ID: "async_compact", Enabled: true, ExpectedRevision: "1"})
	if err != nil || v.SaveState != "pending" {
		t.Fatal("in-flight intent not retained", err, v)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("network cancellation was delayed")
	}
}

func TestExperimentsJournalWaitIsCancellable(t *testing.T) {
	f := newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "profile")
	c, info, err := experimentalClient(t.Context(), f.settings)
	if err != nil {
		t.Fatal(err)
	}
	c.http.CloseIdleConnections()
	unlock, err := lockExperimentJournal(t.Context(), experimentPath(dir, info))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	for _, write := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
		start := time.Now()
		if write {
			_, err = ChangeRuntimeExperiments(ctx, f.settings, dir, api.RuntimeExperimentChange{ID: "async_compact", ExpectedRevision: "1"})
		} else {
			_, err = ReadRuntimeExperiments(ctx, f.settings, dir)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
			t.Fatalf("wait did not cancel promptly: write=%t err=%v", write, err)
		}
	}
	if f.count() != 0 {
		t.Fatal("cancelled admission wrote")
	}
}

func TestExperimentsLateReceiptCannotReplaceNewIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "experiments.json")
	old := experimentJournal{Request: &wire.ExperimentalRequest{OperationId: "original"}, Outcome: "pending"}
	current := experimentJournal{Request: &wire.ExperimentalRequest{OperationId: "new-explicit-action"}, Outcome: "pending"}
	if err := privateWrite(path, current); err != nil {
		t.Fatal(err)
	}
	if err := recordExperiment(t.Context(), path, &old, wire.CommandResult{OperationId: "original", Outcome: "committed"}); err != nil {
		t.Fatal(err)
	}
	got, err := readExperimentState(t.Context(), path)
	if err != nil || got.Request.OperationId != current.Request.OperationId || got.Outcome != "pending" {
		t.Fatal("late reply replaced newer intent", got, err)
	}
}

func TestExperimentsFollowCatalogAndRetireLegacyControls(t *testing.T) {
	f := newExperimentFixture(t)
	dir := t.TempDir()
	f.mu.Lock()
	f.features = []string{"async_compact", "mcp_catalog_refresh", "deferred_tool_retirement"}
	f.values = wire.ExperimentalValues{AsyncCompact: pointer(false), McpCatalogRefresh: pointer(false), DeferredToolRetirement: pointer(true)}
	f.mu.Unlock()
	v := readFixtureExperiments(t, f, dir)
	if len(v.Features) != 1 || v.Features[0].ID != "async_compact" || v.Features[0].Enabled {
		t.Fatal(v)
	}
	for _, id := range []string{"idle_compact", "mcp_catalog_refresh", "deferred_tool_retirement"} {
		if _, err := ChangeRuntimeExperiments(t.Context(), f.settings, dir, api.RuntimeExperimentChange{ID: id, Enabled: true, ExpectedRevision: v.Revision}); err == nil {
			t.Fatalf("wrote unavailable control %s", id)
		}
	}
	if f.count() != 0 {
		t.Fatal("unsupported control wrote")
	}
	f.mu.Lock()
	f.features = append([]string{}, experimentalIDs...)
	f.values.IdleCompact = pointer(false)
	f.mu.Unlock()
	v = readFixtureExperiments(t, f, dir)
	if len(v.Features) != 2 || v.Features[1].ID != "idle_compact" || v.Features[1].Enabled {
		t.Fatal(v)
	}
	f.mu.Lock()
	f.values.IdleCompact = nil
	f.mu.Unlock()
	if _, err := ReadRuntimeExperiments(t.Context(), f.settings, dir); err == nil {
		t.Fatal("missing desired value was treated as false")
	}
}

func TestExperimentsLegacyUnknownReceiptSurvivesRetirement(t *testing.T) {
	f := newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "profile")
	path := experimentPath(dir, wire.ServerInfo{StoreId: pointer("store")})
	j := experimentJournal{Request: &wire.ExperimentalRequest{OperationId: "legacy-save", ExpectedRevision: "1", Values: wire.ExperimentalValues{McpCatalogRefresh: pointer(false)}}, Outcome: "pending"}
	if err := privateWrite(path, j); err != nil {
		t.Fatal(err)
	}
	v := readFixtureExperiments(t, f, dir)
	if v.SaveState != "pending" || v.OperationID != "legacy-save" || len(v.Features) != 2 {
		t.Fatal(v)
	}
	f.mu.Lock()
	f.receipts["legacy-save"] = wire.CommandResult{OperationId: "legacy-save", Outcome: "committed"}
	f.mu.Unlock()
	v = readFixtureExperiments(t, f, dir)
	if v.SaveState != "saved" || v.OperationID != "legacy-save" || f.count() != 0 {
		t.Fatal(v)
	}
	saved, err := readExperimentJournal(path)
	if err != nil || saved.Request.Values.McpCatalogRefresh == nil || *saved.Request.Values.McpCatalogRefresh {
		t.Fatal("lost legacy receipt payload", saved, err)
	}
}
