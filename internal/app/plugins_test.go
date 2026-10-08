package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
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
	reads      int
	generation uint64
	fail       bool
}

func (e *inspectingPluginEngine) BotPluginServer(context.Context, string) (plugins.ServerDetail, error) {
	e.reads++
	if e.fail {
		return plugins.ServerDetail{}, errors.New("fixture failure")
	}
	return plugins.ServerDetail{State: "connected", Tools: []plugins.Tool{{Name: "lookup", Description: "Find a note"}}}, nil
}
func (e *inspectingPluginEngine) BotPluginGeneration() uint64 { return e.generation }

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
