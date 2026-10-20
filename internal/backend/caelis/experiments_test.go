package caelis

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

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
}

func newExperimentFixture(t *testing.T) *experimentFixture {
	f := &experimentFixture{revision: 1, values: wire.ExperimentalValues{AsyncCompact: pointer(false), McpCatalogRefresh: pointer(false), DeferredToolRetirement: pointer(false)}, receipts: map[string]wire.CommandResult{}}
	f.settings = setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
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
			for _, id := range experimentalIDs {
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
				if req.Values.McpCatalogRefresh != nil {
					f.values.McpCatalogRefresh = req.Values.McpCatalogRefresh
				}
				if req.Values.DeferredToolRetirement != nil {
					f.values.DeferredToolRetirement = req.Values.DeferredToolRetirement
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
func initializeFixtureExperiments(t *testing.T, f *experimentFixture, dir string) {
	t.Helper()
	c, info, err := experimentalClient(t.Context(), f.settings)
	if err != nil {
		t.Fatal(err)
	}
	defer c.http.CloseIdleConnections()
	if err = initializeExperiments(t.Context(), c, info, dir); err != nil {
		t.Fatal(err)
	}
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
func TestExperimentsDefaultOnceAndIndependentUserChoices(t *testing.T) {
	f := newExperimentFixture(t)
	dir := filepath.Join(t.TempDir(), "profile")
	readFixtureExperiments(t, f, dir)
	if f.count() != 0 {
		t.Fatal("page read initialized defaults")
	}
	initializeFixtureExperiments(t, f, dir)
	v := readFixtureExperiments(t, f, dir)
	for _, feature := range v.Features {
		if !feature.Enabled {
			t.Fatal("default was not enabled", feature)
		}
	}
	v = changeFixtureExperiment(t, f, dir, "async_compact", false, v.Revision)
	if v.SaveState != "saved" || v.Features[0].Enabled || !v.Features[1].Enabled || !v.Features[2].Enabled {
		t.Fatal(v)
	}
	before := f.count()
	// Both restart in the same Bot profile and another Bot profile honor Core's
	// deterministic initialization receipt after the user's choice changed.
	initializeFixtureExperiments(t, f, dir)
	initializeFixtureExperiments(t, f, filepath.Join(t.TempDir(), "other-profile"))
	v = readFixtureExperiments(t, f, dir)
	if f.count() != before || v.Features[0].Enabled {
		t.Fatal("restart overwrote user choice", v)
	}
	stale := changeFixtureExperiment(t, f, dir, "mcp_catalog_refresh", false, "1")
	if stale.SaveState != "conflicted" || f.count() != before {
		t.Fatal("stale tab wrote", stale)
	}
	v = changeFixtureExperiment(t, f, dir, "mcp_catalog_refresh", false, v.Revision)
	if v.Features[1].Enabled || !v.Features[2].Enabled {
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
			initializeFixtureExperiments(t, f, dir)
			for range 2 {
				v = readFixtureExperiments(t, f, dir)
				if v.SaveState != "pending" {
					t.Fatal("wrong or unknown receipt settled", v)
				}
			}
			v = changeFixtureExperiment(t, f, dir, "mcp_catalog_refresh", true, v.Revision)
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
			v = changeFixtureExperiment(t, f, dir, "mcp_catalog_refresh", true, v.Revision)
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
	initializeFixtureExperiments(t, f, dir)
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
