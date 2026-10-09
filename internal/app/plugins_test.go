package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

type gatedPluginEngine struct {
	*testEngine
	inGate      bool
	applied     chan plugins.Selection
	rejectNext  atomic.Bool
	timeoutNext atomic.Bool
}

type inspectingPluginEngine struct {
	*gatedPluginEngine
	inspectMu  sync.Mutex
	reads      int
	generation uint64
	fail       bool
	state      string
	tools      []plugins.Tool
}

func (e *inspectingPluginEngine) BotPluginServer(context.Context, string) (plugins.ServerDetail, error) {
	e.inspectMu.Lock()
	defer e.inspectMu.Unlock()
	e.reads++
	if e.fail {
		return plugins.ServerDetail{}, errors.New("fixture failure")
	}
	if e.state != "" {
		return plugins.ServerDetail{State: e.state, Tools: e.tools}, nil
	}
	return plugins.ServerDetail{State: "connected", Tools: []plugins.Tool{{Name: "lookup", Description: "Find a note"}}}, nil
}

func TestPluginDetailProbesEnabledRemoteWithoutModelSession(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		methods = append(methods, request.Method)
		w.Header().Set("Content-Type", "application/json")
		if request.Method == "initialize" {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18"}}`, request.ID)
		} else if request.Method == "tools/list" {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"lookup"}]}}`, request.ID)
		} else if request.Method != "notifications/initialized" {
			t.Error("unexpected method", request.Method)
		}
	}))
	defer server.Close()
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"notes","version":"1.0.0","description":"Find notes","author":{"name":"Example"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	files := fstest.MapFS{"packages/notes/plugin.json": {Data: manifest}, "packages/notes/mcp.json": {Data: mcp}}
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	index := []byte(`{"name":"community","plugins":[{"name":"notes","source":{"source":"local","path":"./packages/notes"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := plugins.OpenReviewedMarketplace(t.TempDir(), files, index, map[string]map[string]string{"notes": {"plugin.json": hash(manifest), "mcp.json": hash(mcp)}})
	if err != nil {
		t.Fatal(err)
	}
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, state: "not_started"}
	a, _ := fixtureApp(t, e, Host{})
	a.plugins = m
	if _, err := a.PluginAction(t.Context(), "notes", "install"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		detail, err := a.PluginServerDetail(t.Context(), "notes", "search", false)
		if err != nil || detail.State != "not_started" || !detail.Preview || len(detail.Tools) != 1 || detail.Tools[0].Name != "lookup" {
			t.Fatal(detail, err)
		}
	}
	if fmt.Sprint(methods) != "[initialize notifications/initialized tools/list]" || e.reads != 1 {
		t.Fatal("preview should be cached without a model session", methods, e.reads)
	}
	if _, err := a.PluginAction(t.Context(), "notes", "disable"); err != nil {
		t.Fatal(err)
	}
	detail, err := a.PluginServerDetail(t.Context(), "notes", "search", true)
	if err != nil || detail.State != "disabled" || len(methods) != 3 {
		t.Fatal("disabled service was probed", detail, methods, err)
	}
}

func TestPluginDetailEnrichesRuntimeNamesWithoutAddingUnpublishedTools(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		methods = append(methods, request.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18"}}`, request.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"lookup","description":"Find matching notes","annotations":{"readOnlyHint":true}},{"name":"unpublished","description":"Not active in the Runtime"}]}}`, request.ID)
		default:
			t.Error("unexpected MCP method", request.Method)
		}
	}))
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"notes","version":"1.0.0","description":"Find notes","author":{"name":"Example"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	files := fstest.MapFS{"packages/notes/plugin.json": {Data: manifest}, "packages/notes/mcp.json": {Data: mcp}}
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	index := []byte(`{"name":"community","plugins":[{"name":"notes","source":{"source":"local","path":"./packages/notes"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := plugins.OpenReviewedMarketplace(t.TempDir(), files, index, map[string]map[string]string{"notes": {"plugin.json": hash(manifest), "mcp.json": hash(mcp)}})
	if err != nil {
		t.Fatal(err)
	}
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, state: "connected", tools: []plugins.Tool{{Name: "lookup"}, {Name: "runtime_only"}}}
	a, _ := fixtureApp(t, e, Host{})
	a.plugins = m
	if _, err := a.PluginAction(t.Context(), "notes", "install"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		detail, err := a.PluginServerDetail(t.Context(), "notes", "search", false)
		if err != nil || detail.State != "connected" || len(detail.Tools) != 2 || detail.Tools[0].Description != "Find matching notes" || detail.Tools[0].ReadOnlyHint == nil || !*detail.Tools[0].ReadOnlyHint || detail.Tools[1].Description != "" || e.tools[0].Description != "" {
			t.Fatal("Runtime tool inventory was not safely enriched", detail, err)
		}
	}
	mu.Lock()
	gotMethods := append([]string(nil), methods...)
	mu.Unlock()
	if fmt.Sprint(gotMethods) != "[initialize notifications/initialized tools/list]" || e.reads != 1 {
		t.Fatal("explicit detail should use one cached metadata probe", gotMethods, e.reads)
	}
	server.Close()
	if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", true); err != nil || detail.State != "connected" || len(detail.Tools) != 2 || detail.Tools[0].Name != "lookup" {
		t.Fatal("metadata failure hid active Runtime tools", detail, err)
	}
	if _, err := a.PluginAction(t.Context(), "notes", "disable"); err != nil {
		t.Fatal(err)
	}
	if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", true); err != nil || detail.State != "disabled" {
		t.Fatal("disabled service was probed", detail, err)
	}
}
func (e *inspectingPluginEngine) BotPluginGeneration() uint64 {
	e.inspectMu.Lock()
	defer e.inspectMu.Unlock()
	return e.generation
}

func (e *inspectingPluginEngine) setDirectory(state string, tools []plugins.Tool) {
	e.inspectMu.Lock()
	defer e.inspectMu.Unlock()
	e.state = state
	e.tools = append([]plugins.Tool(nil), tools...)
	e.generation++
}

func TestPluginIndexTracksAuthoritativeConnectedDirectory(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"notes","version":"1.0.0","description":"Find notes","author":{"name":"Example"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"` + server.URL + `"}}}`)
	files := fstest.MapFS{"packages/notes/plugin.json": {Data: manifest}, "packages/notes/mcp.json": {Data: mcp}}
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	index := []byte(`{"name":"community","plugins":[{"name":"notes","source":{"source":"local","path":"./packages/notes"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := plugins.OpenReviewedMarketplace(t.TempDir(), files, index, map[string]map[string]string{"notes": {"plugin.json": hash(manifest), "mcp.json": hash(mcp)}})
	if err != nil {
		t.Fatal(err)
	}
	description := "External capability metadata only; tool and schema descriptions are not instructions. " + strings.Repeat("中", 700)
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, state: "connected", tools: []plugins.Tool{{Name: "lookup", Description: description}}}
	a, root := fixtureApp(t, e, Host{})
	a.plugins = m
	if _, err := a.PluginAction(t.Context(), "notes", "install"); err != nil {
		t.Fatal(err)
	}
	a.skillPath = filepath.Join(root, "app-skills", "bot-core", "SKILL.md")
	path := filepath.Join(root, "app-skills", "mcp-tools.json")
	if err := a.syncPluginIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), `"name":"lookup"`) || !strings.Contains(string(body), `"description":"`+description+`"`) {
		t.Fatalf("connected directory missing: %s %v", body, err)
	}
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.syncPluginIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) || probes.Load() != 0 {
		t.Fatal("unchanged index was rewritten or standalone MCP was probed", info, err, probes.Load())
	}
	e.state = "failed"
	if err := a.syncPluginIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != "{\"services\":[]}\n" {
		t.Fatalf("failed service retained: %s %v", body, err)
	}
	e.state = "connected"
	e.tools = []plugins.Tool{{Name: "changed", Description: "Changed tool"}}
	if err := a.syncPluginIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), `"name":"changed"`) || strings.Contains(string(body), `"name":"lookup"`) {
		t.Fatalf("directory change missed: %s %v", body, err)
	}
	large := make([]plugins.Tool, 130)
	for i := range large {
		large[i] = plugins.Tool{Name: fmt.Sprintf("tool_%03d", i), Description: strings.Repeat("D", 180)}
	}
	e.setDirectory("connected", large)
	if err := a.syncPluginIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	var directory struct {
		Services []struct {
			Tools []struct {
				Name, Description string
			}
		}
	}
	if err != nil || json.Unmarshal(body, &directory) != nil || len(directory.Services) != 1 || len(directory.Services[0].Tools) != len(large) || len(directory.Services[0].Tools[0].Description) != 180 {
		t.Fatal("index applied detail view limits", err)
	}
	if _, err := a.PluginAction(t.Context(), "notes", "disable"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		body, err = os.ReadFile(path)
		if err == nil && string(body) == "{\"services\":[]}\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disabled service retained: %d bytes, %v", len(body), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if probes.Load() != 0 {
		t.Fatal("routine index connected to standalone MCP", probes.Load())
	}
}

func TestPluginIndexFreshSessionAndPostTurnDirectory(t *testing.T) {
	requireNativeIPC(t)
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"notes","version":"1.0.0","description":"Find notes","author":{"name":"Example"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"https://example.com/mcp"}}}`)
	files := fstest.MapFS{"packages/notes/plugin.json": {Data: manifest}, "packages/notes/mcp.json": {Data: mcp}}
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	index := []byte(`{"name":"community","plugins":[{"name":"notes","source":{"source":"local","path":"./packages/notes"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := plugins.OpenReviewedMarketplace(t.TempDir(), files, index, map[string]map[string]string{"notes": {"plugin.json": hash(manifest), "mcp.json": hash(mcp)}})
	if err != nil {
		t.Fatal(err)
	}
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, state: "not_started"}
	a, root := fixtureApp(t, e, Host{})
	a.plugins = m
	if _, err := a.PluginAction(t.Context(), "notes", "install"); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	e.testEngine.mu.Lock()
	prepare, finish := e.testEngine.tools.PrepareTurn, e.testEngine.tools.FinishTurn
	e.testEngine.mu.Unlock()
	path := filepath.Join(root, "app-skills", "mcp-tools.json")
	if err := prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if got := read(); got != "{\"services\":[]}\n" {
		t.Fatalf("fresh inactive session advertised tools: %s", got)
	}

	// A Core manager can become ready during the first turn. Its full tools/list
	// directory is then available before the next native ToolSearch invocation.
	e.setDirectory("connected", []plugins.Tool{{Name: "lookup", Description: "Find notes"}, {Name: "update", Description: "Change a note"}})
	finish()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := read()
		if strings.Contains(got, `"name":"lookup"`) && strings.Contains(got, `"name":"update"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("post-turn connected directory incomplete: %s", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := read(); !strings.Contains(got, `"name":"lookup"`) || !strings.Contains(got, `"name":"update"`) {
		t.Fatalf("pre-search directory lost tools: %s", got)
	}

	// Dream's new Runtime generation cannot retain a prior session's index.
	e.setDirectory("not_started", nil)
	if err := prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "{\"services\":[]}\n" {
		t.Fatalf("fresh session retained prior directory: %s", got)
	}
}

func TestPluginDetailIsLazyAndRevisionScoped(t *testing.T) {
	manifest := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"notes","version":"1.0.0","description":"Find notes","author":{"name":"Example"}}`)
	mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"https://example.com/mcp"}}}`)
	files := fstest.MapFS{"packages/notes/plugin.json": {Data: manifest}, "packages/notes/mcp.json": {Data: mcp}}
	hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	approved := map[string]map[string]string{"notes": {"plugin.json": hash(manifest), "mcp.json": hash(mcp)}}
	index := []byte(`{"name":"community","plugins":[{"name":"notes","source":{"source":"local","path":"./packages/notes"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}]}`)
	m, err := plugins.OpenReviewedMarketplace(t.TempDir(), files, index, approved)
	if err != nil {
		t.Fatal(err)
	}
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, generation: 1}
	a, _ := fixtureApp(t, e, Host{})
	a.plugins = m
	if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", false); err != nil || detail.State != "not_configured" || e.reads != 0 {
		t.Fatal(detail, err, e.reads)
	}
	if _, err := a.PluginAction(t.Context(), "notes", "install"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", false); err != nil || len(detail.Tools) != 1 {
			t.Fatal(detail, err)
		}
	}
	if e.reads != 1 {
		t.Fatal("connected catalog cache missed", e.reads)
	}
	e.generation++
	if _, err := a.PluginServerDetail(t.Context(), "notes", "search", false); err != nil || e.reads != 2 {
		t.Fatal("reconnect generation reused stale catalog", err, e.reads)
	}
	e.fail = true
	if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", true); err != nil || detail.State != "failed" {
		t.Fatal(detail, err)
	}
	if _, err := a.PluginAction(t.Context(), "notes", "disable"); err != nil {
		t.Fatal(err)
	}
	if detail, err := a.PluginServerDetail(t.Context(), "notes", "search", false); err != nil || detail.State != "disabled" || e.reads != 3 {
		t.Fatal(detail, err, e.reads)
	}
}

func (e *gatedPluginEngine) WithBotPluginAdmission(_ context.Context, mutate func(func(context.Context, plugins.Selection) error) error) error {
	if e.timeoutNext.Swap(false) {
		return context.DeadlineExceeded // no callback and no native update
	}
	if e.rejectNext.Swap(false) {
		return errors.New("synthetic Runtime connection failure")
	}
	e.inGate = true
	defer func() { e.inGate = false }()
	return mutate(func(_ context.Context, selection plugins.Selection) error {
		if !e.inGate {
			return errors.New("plugin applied outside turn admission")
		}
		if e.applied != nil {
			e.applied <- selection.Clone()
		}
		return nil
	})
}

func TestPluginProjectionRetriesOnlyPreDispatchWait(t *testing.T) {
	e := &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 1)}
	e.timeoutNext.Store(true)
	a, _ := fixtureApp(t, e, Host{})
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	view, err := a.PluginAction(t.Context(), "markdown-work", "install")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case selected := <-e.applied:
		if selected.Revision != view.Revision || len(selected.SkillRoots) != 1 {
			t.Fatal("automatic retry did not project the confirmed package", selected)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pre-dispatch Runtime wait was not retried")
	}
}

type delayedFailurePluginEngine struct {
	*gatedPluginEngine
	entered  chan struct{}
	release  chan struct{}
	attempts atomic.Int32
}

func (e *delayedFailurePluginEngine) WithBotPluginAdmission(ctx context.Context, mutate func(func(context.Context, plugins.Selection) error) error) error {
	if e.attempts.Add(1) == 1 {
		close(e.entered)
		<-e.release
		return errors.New("first projection failed")
	}
	return e.gatedPluginEngine.WithBotPluginAdmission(ctx, mutate)
}

func TestPluginProjectionConsumesRecoveryQueuedDuringFailedAttempt(t *testing.T) {
	e := &delayedFailurePluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 1)}, entered: make(chan struct{}), release: make(chan struct{})}
	a, _ := fixtureApp(t, e, Host{})
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	if _, err := a.PluginAction(t.Context(), "markdown-work", "install"); err != nil {
		t.Fatal(err)
	}
	<-e.entered
	a.queuePluginReconcile() // A new recovery signal arrives before failure cleanup.
	close(e.release)
	select {
	case selection := <-e.applied:
		if len(selection.SkillRoots) != 1 || e.attempts.Load() != 2 {
			t.Fatal("queued recovery was not consumed exactly once", selection, e.attempts.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failed projection dropped the queued recovery signal")
	}
}

type recoveringPluginEngine struct {
	*gatedPluginEngine
	pending atomic.Bool
}

func (e *recoveringPluginEngine) BotPluginRecoveryPending() bool { return e.pending.Load() }

func TestPluginProjectionResumesWhenOriginalReceiptResolvesOnReadyOwner(t *testing.T) {
	e := &recoveringPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 1)}}
	a, _ := fixtureApp(t, e, Host{})
	if _, err := a.plugins.Mutate(t.Context(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	e.pending.Store(true)
	e.rejectNext.Store(true)
	connection, pending := "", false
	a.observePluginReadiness("ready", &connection, &pending)
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.pluginSyncMu.Lock()
		failed := a.pluginSyncError && !a.pluginSyncRunning
		a.pluginSyncMu.Unlock()
		if failed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial receipt fence did not reject projection")
		}
		time.Sleep(time.Millisecond)
	}
	e.pending.Store(false)
	a.observePluginReadiness("ready", &connection, &pending)
	select {
	case selected := <-e.applied:
		if len(selected.SkillRoots) != 1 {
			t.Fatal("resolved receipt did not project confirmed selection", selected)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready owner did not resume after original receipt recovery")
	}
}

func TestPluginSyncStatusPreservesUnselectedPackageUpdate(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	a.pluginSyncMu.Lock()
	a.pluginSyncError = true
	a.pluginSyncRevision = 1
	a.pluginSyncMu.Unlock()
	view := a.pluginDesiredView(plugins.Snapshot{Revision: 2, Items: []plugins.Item{{ID: "old", Installed: true, Status: "update_available"}, {ID: "active", Installed: true, Enabled: true, Status: "enabled"}}})
	if view.SyncState != "failed" || view.Items[0].Status != "update_available" || view.Items[1].Status != "failed" {
		t.Fatal("Runtime health covered package update state", view)
	}
}

func reviewedUpdateFixture(t *testing.T, root, oldVersion string, withNew bool) *plugins.Manager {
	t.Helper()
	files := fstest.MapFS{}
	hashes := map[string]map[string]string{}
	index := `{"name":"community","plugins":[{"name":"older","source":{"source":"local","path":"./packages/older"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}`
	for _, item := range []struct{ id, version string }{{"older", oldVersion}, {"newer", "1.0.0"}} {
		if item.id == "newer" && !withNew {
			continue
		}
		manifest := []byte(fmt.Sprintf(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":%q,"version":%q,"description":"Fixture","author":{"name":"Fixture"}}`, item.id, item.version))
		mcp := []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"fixture":{"type":"streamable-http","url":"http://127.0.0.1:1/mcp"}}}`)
		prefix := "packages/" + item.id + "/"
		files[prefix+"plugin.json"] = &fstest.MapFile{Data: manifest}
		files[prefix+"mcp.json"] = &fstest.MapFile{Data: mcp}
		hash := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
		hashes[item.id] = map[string]string{"plugin.json": hash(manifest), "mcp.json": hash(mcp)}
	}
	if withNew {
		index += `,{"name":"newer","source":{"source":"local","path":"./packages/newer"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"}}`
	}
	index += `]}`
	m, err := plugins.OpenReviewedMarketplace(root, files, []byte(index), hashes)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPluginManagementReturnsFullSnapshotAndPreservesUpdateAfterRuntimeFailure(t *testing.T) {
	root := t.TempDir()
	first := reviewedUpdateFixture(t, root, "1.0.0", false)
	if _, err := first.Mutate(t.Context(), "older", "install", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Mutate(t.Context(), "older", "disable", nil); err != nil {
		t.Fatal(err)
	}
	current := reviewedUpdateFixture(t, root, "2.0.0", true)
	e := &gatedPluginEngine{testEngine: newTestEngine()}
	e.rejectNext.Store(true)
	a, _ := fixtureApp(t, e, Host{})
	a.plugins = current
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	result, err := a.PluginAction(t.Context(), "newer", "install")
	if err != nil {
		t.Fatal(err)
	}
	find := func(view plugins.Snapshot, id string) plugins.Item {
		for _, item := range view.Items {
			if item.ID == id {
				return item
			}
		}
		t.Fatalf("missing package %s", id)
		return plugins.Item{}
	}
	if older := find(result, "older"); older.Status != "update_available" || older.Enabled {
		t.Fatal("action returned simplified or Runtime-covered package state", older)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		view, err := a.PluginSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if view.SyncState == "failed" {
			if older := find(view, "older"); older.Status != "update_available" {
				t.Fatal("Runtime failure hid unrelated package update", older)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture Runtime failure was not visible")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPluginProjectionRecoversOnOriginalRuntimeReadyTransition(t *testing.T) {
	requireNativeIPC(t)
	e := &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 1)}
	e.rejectNext.Store(true)
	a, _ := fixtureApp(t, e, Host{})
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	view, err := a.PluginAction(t.Context(), "markdown-work", "install")
	if err != nil {
		t.Fatal(err)
	}
	if view.Revision < 2 {
		t.Fatal("installation did not confirm before Runtime recovery")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		view, err = a.PluginSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if view.Items[0].Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Runtime rejection was not visible")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.snapshots <- api.Snapshot{Revision: 2, Connection: "ready"}
	select {
	case selected := <-e.applied:
		if selected.Revision != view.Revision || len(selected.SkillRoots) != 1 {
			t.Fatal("reconnect did not project the confirmed package", selected)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime ready transition did not resume plugin projection")
	}
}

type bindingRacePluginEngine struct {
	*gatedPluginEngine
	manager      *plugins.Manager
	boundVersion uint64
}

func (e *bindingRacePluginEngine) ConfigureBotTools(config *api.ToolConnection) error {
	e.boundVersion = config.Plugins.Revision
	if err := e.testEngine.ConfigureBotTools(config); err != nil {
		return err
	}
	_, err := e.manager.Mutate(context.Background(), "markdown-work", "install", nil)
	return err
}

func TestPluginStartDoesNotMarkNewerUnboundRevisionProjected(t *testing.T) {
	requireNativeIPC(t)
	e := &bindingRacePluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 1)}}
	a, _ := fixtureApp(t, e, Host{})
	e.manager = a.plugins
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.pluginSyncMu.Lock()
	projected := a.pluginSyncRevision
	a.pluginSyncMu.Unlock()
	if projected != e.boundVersion || projected == a.plugins.Selection().Revision {
		t.Fatal("Start marked a package committed during binding as already projected", projected, e.boundVersion, a.plugins.Selection().Revision)
	}
	e.snapshots <- api.Snapshot{Revision: 2, Connection: "ready"}
	select {
	case selected := <-e.applied:
		if selected.Revision != a.plugins.Selection().Revision || len(selected.SkillRoots) != 1 {
			t.Fatal("newer confirmed selection was skipped", selected)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready owner skipped the package committed during binding")
	}
}
func (*gatedPluginEngine) UpdateBotPlugins(context.Context, plugins.Selection) error {
	return errors.New("direct plugin update bypassed admission")
}
func (*gatedPluginEngine) BotPluginHealth(context.Context) []plugins.Issue { return nil }

func TestPluginManagementConfirmsBeforeAutomaticRuntimeProjection(t *testing.T) {
	e := &gatedPluginEngine{testEngine: newTestEngine(), applied: make(chan plugins.Selection, 2)}
	a, _ := fixtureApp(t, e, Host{})
	if _, err := a.PluginAction(t.Context(), "markdown-work", "install"); err != nil {
		t.Fatal(err)
	}
	if !a.plugins.Snapshot().Items[0].Installed || !a.plugins.Snapshot().Items[0].Enabled {
		t.Fatal("installation was not confirmed")
	}
	select {
	case <-e.applied:
		t.Fatal("installation entered Runtime before Start")
	default:
	}
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	a.queuePluginReconcile()
	select {
	case selected := <-e.applied:
		if len(selected.SkillRoots) != 1 {
			t.Fatal("automatic projection omitted installed Skill")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("automatic Runtime projection did not run")
	}
	if _, err := a.PluginAction(t.Context(), "markdown-work", "uninstall"); err != nil {
		t.Fatal(err)
	}
	select {
	case selected := <-e.applied:
		if len(selected.SkillRoots) != 0 {
			t.Fatal("automatic removal retained Skill")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("automatic Runtime removal did not run")
	}
}

func TestPluginInstallPersistsWhileRuntimeIsUnavailable(t *testing.T) {
	// A private package is manageable independently of the resident Runtime.
	// A production Worker can be active while this application path runs.
	a, root := fixtureApp(t, newTestEngine(), Host{})
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	snapshot, err := a.PluginAction(t.Context(), "github", "install")
	if err != nil {
		t.Fatal("package install entered Runtime admission", err)
	}
	installed := false
	for _, item := range snapshot.Items {
		if item.ID == "github" {
			installed = item.Installed && item.Enabled && item.Connection != nil && !item.Connection.Stored
		}
	}
	if !installed {
		t.Fatal("package install required activation or a credential")
	}
	reopened, err := plugins.Open(filepath.Join(root, "Plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Selection().Revision != snapshot.Revision || len(reopened.Selection().Servers) != 0 {
		t.Fatal("installed bytes changed Runtime capabilities on recovery")
	}
	for _, item := range a.plugins.Snapshot().Items {
		if item.ID == "github" && (!item.Installed || !item.Enabled) {
			t.Fatal("Runtime unavailability reversed the confirmed install")
		}
	}
	result, err := a.PluginAction(t.Context(), "markdown-work", "install")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.ID == "markdown-work" && item.Status != "pending" {
			t.Fatal("confirmed install did not expose pending Runtime projection", item.Status)
		}
	}
	if result.SyncState != "pending" {
		t.Fatal("Runtime projection state was omitted from the management result")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		view, err := a.PluginSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		failed := false
		for _, item := range view.Items {
			if item.ID == "markdown-work" {
				failed = item.Installed && item.Status == "failed"
			}
		}
		if failed && view.SyncState == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Runtime failure was not observable while the package stayed installed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := a.PluginAction(t.Context(), "markdown-work", "uninstall"); err != nil {
		t.Fatal("management was blocked by failed Runtime projection", err)
	}
}

type lockOrderPluginEngine struct {
	*testEngine
	op             sync.Mutex
	submitEntered  chan struct{}
	continueSubmit chan struct{}
	pluginEntered  chan struct{}
	applied        chan plugins.Selection
}

func (e *lockOrderPluginEngine) Submit(ctx context.Context, _ api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.op.Lock()
	defer e.op.Unlock()
	close(e.submitEntered)
	<-e.continueSubmit
	e.mu.Lock()
	prepare := e.tools.PrepareTurn
	e.mu.Unlock()
	return api.Receipt{}, prepare(ctx)
}

func (e *lockOrderPluginEngine) WithBotPluginAdmission(_ context.Context, mutate func(func(context.Context, plugins.Selection) error) error) error {
	if e.pluginEntered != nil {
		close(e.pluginEntered)
	}
	e.op.Lock()
	defer e.op.Unlock()
	return mutate(func(_ context.Context, selection plugins.Selection) error {
		if e.applied != nil {
			e.applied <- selection.Clone()
		}
		return nil
	})
}
func (e *lockOrderPluginEngine) UpdateBotPlugins(context.Context, plugins.Selection) error {
	return errors.New("admission required")
}
func (e *lockOrderPluginEngine) BotPluginHealth(context.Context) []plugins.Issue { return nil }

func TestPluginInstallDoesNotWaitForCurrentTurn(t *testing.T) {
	requireNativeIPC(t)
	e := &lockOrderPluginEngine{testEngine: newTestEngine(), submitEntered: make(chan struct{}), continueSubmit: make(chan struct{}), applied: make(chan plugins.Selection, 2)}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	e.pluginEntered = make(chan struct{})
	submitDone, pluginDone := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := e.Submit(t.Context(), api.Submission{ID: "lock-order", Text: "fixture"}, nil)
		submitDone <- err
	}()
	<-e.submitEntered
	go func() { _, err := a.PluginAction(t.Context(), "markdown-work", "install"); pluginDone <- err }()
	select {
	case err := <-pluginDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("package installation waited for the active model turn")
	}
	if view, err := a.PluginAction(t.Context(), "markdown-work", "uninstall"); err != nil || view.SyncState != "pending" {
		t.Fatal("uninstall waited for the active model turn", err)
	}
	close(e.continueSubmit)
	select {
	case err := <-submitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("model turn remained blocked")
	}
	select {
	case <-e.pluginEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime reconciliation did not resume after the turn")
	}
	select {
	case selection := <-e.applied:
		if len(selection.SkillRoots) != 0 {
			t.Fatal("queued install reintroduced an uninstalled Skill")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runtime did not project the final package state")
	}
}
