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
	inGate, applied bool
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
	e := &inspectingPluginEngine{gatedPluginEngine: &gatedPluginEngine{testEngine: newTestEngine()}, state: "connected", tools: []plugins.Tool{{Name: "lookup", Description: "Find notes"}}}
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
	if err != nil || !strings.Contains(string(body), `"name":"lookup"`) || !strings.Contains(string(body), `"description":"Find notes"`) {
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
	body, err = os.ReadFile(path)
	if err != nil || string(body) != "{\"services\":[]}\n" {
		t.Fatalf("disabled service retained: %s %v", body, err)
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

func (e *gatedPluginEngine) WithBotPluginAdmission(mutate func(func(context.Context, plugins.Selection) error) error) error {
	e.inGate = true
	defer func() { e.inGate = false }()
	return mutate(func(context.Context, plugins.Selection) error {
		if !e.inGate {
			return errors.New("plugin applied outside turn admission")
		}
		e.applied = true
		return nil
	})
}
func (*gatedPluginEngine) UpdateBotPlugins(context.Context, plugins.Selection) error {
	return errors.New("direct plugin update bypassed admission")
}
func (*gatedPluginEngine) BotPluginHealth(context.Context) []plugins.Issue { return nil }

func TestPluginActionUsesRuntimeAdmissionThroughStateConfirmation(t *testing.T) {
	e := &gatedPluginEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	if _, err := a.PluginAction(t.Context(), "markdown-work", "install"); err != nil {
		t.Fatal(err)
	}
	if e.applied || !a.plugins.Snapshot().Items[0].Enabled {
		t.Fatal("pre-start installation unexpectedly entered Runtime")
	}
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	if _, err := a.PluginAction(t.Context(), "markdown-work", "disable"); err != nil {
		t.Fatal(err)
	}
	if !e.applied || a.plugins.Snapshot().Items[0].Enabled {
		t.Fatal("Runtime admission did not cover active plugin mutation")
	}
}

type lockOrderPluginEngine struct {
	*testEngine
	op             sync.Mutex
	submitEntered  chan struct{}
	continueSubmit chan struct{}
	pluginEntered  chan struct{}
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

func (e *lockOrderPluginEngine) WithBotPluginAdmission(mutate func(func(context.Context, plugins.Selection) error) error) error {
	close(e.pluginEntered)
	e.op.Lock()
	defer e.op.Unlock()
	return mutate(func(context.Context, plugins.Selection) error { return nil })
}
func (e *lockOrderPluginEngine) UpdateBotPlugins(context.Context, plugins.Selection) error {
	return errors.New("admission required")
}
func (e *lockOrderPluginEngine) BotPluginHealth(context.Context) []plugins.Issue { return nil }

func TestPluginActionAndPrepareTurnUseRuntimeBeforeAppLock(t *testing.T) {
	requireNativeIPC(t)
	e := &lockOrderPluginEngine{testEngine: newTestEngine(), submitEntered: make(chan struct{}), continueSubmit: make(chan struct{}), pluginEntered: make(chan struct{})}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	submitDone, pluginDone := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := e.Submit(t.Context(), api.Submission{ID: "lock-order", Text: "fixture"}, nil)
		submitDone <- err
	}()
	<-e.submitEntered
	go func() { _, err := a.PluginAction(t.Context(), "markdown-work", "install"); pluginDone <- err }()
	<-e.pluginEntered
	close(e.continueSubmit)
	for _, done := range []<-chan error{submitDone, pluginDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("runtime/app lock order deadlocked")
		}
	}
}
