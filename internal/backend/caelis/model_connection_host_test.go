package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Opt-in public-Host acceptance. A live URL only reads Magpie model metadata;
// connection and selection writes remain in a temporary Host. No inference,
// real credential, daily Store or shared-service mutation is involved.
func TestExternalHostMagpieProviderSelection(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for external model-connection acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	environment := newExecutionFixture(t, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, environment.env
	logFile, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err = cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	_ = logFile.Close()
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	waitAcceptance(t, ctx, func() bool { _, _, err := Discover(settings); return err == nil })

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Errorf("directory validation attempted a non-catalog request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "fixture/chat", "kind": "chat", "context_window": 32000, "max_output_tokens": 4096, "reasoning": false}}})
	}))
	defer gateway.Close()
	baseURL, key := gateway.URL+"/v1", "SYNTHETIC_ONLY"
	if live := os.Getenv("CAELIS_BOT_MAGPIE_CATALOG_URL"); live != "" {
		// This opt-in is restricted to the documented unauthenticated local preset.
		if live != "http://127.0.0.1:3425/v1" {
			t.Fatal("live catalog acceptance only supports the local Magpie preset")
		}
		baseURL, key = live, ""
	}
	providers, err := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "providers"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range providers {
		found = found || p.Value == "magpie"
	}
	if !found {
		t.Fatal("Bot provider menu has no Magpie connection")
	}
	models, err := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "models", Provider: "magpie", BaseURL: baseURL})
	if err != nil || len(models) == 0 {
		t.Fatal("Bot could not read the Magpie model menu", err)
	}
	for _, m := range models {
		if strings.Contains(m.Value, "mimo-v2.5-asr") {
			t.Fatal("audio transcription entered the chat-model menu")
		}
	}

	// Select a different provider first so a stale provider field is detectable.
	if err = ApplySetup(ctx, api.SetupRequest{Settings: settings, Action: "connect-model", Provider: "custom", Model: "previous-model", BaseURL: gateway.URL + "/v1", APIKey: "SYNTHETIC_ONLY"}); err != nil {
		t.Fatal(err)
	}
	previous := fixtureModelSelector(t, ctx, mustHostClient(t, settings), "previous-model")
	view, err := ReadRuntimeConfiguration(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ChangeRuntimeConfiguration(ctx, settings, api.RuntimeConfigurationChange{Action: "main", Selection: api.WorkExecutionSettings{Model: previous}, ExpectedRevision: view.Revision}); err != nil || result.Outcome != "committed" {
		t.Fatal("previous provider selection", err)
	}
	connections := &Connections{}
	defer connections.Close()
	flow, err := connections.Start(ctx, settings, api.RuntimeConnectionInput{Kind: "api-key", Choice: "magpie", BaseURL: baseURL, Model: models[0].Value, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitConnection(t, connections, flow.ID, "complete")
	host := mustHostClient(t, settings)
	catalog, err := rawCatalog(ctx, host, "model")
	if err != nil {
		t.Fatal(err)
	}
	selected := ""
	for _, row := range catalog {
		if value(row.Display) == models[0].Value {
			selected = row.Value
			break
		}
	}
	if selected == "" {
		t.Fatal("connected Magpie model was absent from Bot selection catalog")
	}

	// Keep a distinct Host default to verify the application's setting scope.
	baseline, err := ReadRuntimeConfiguration(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ChangeRuntimeConfiguration(ctx, settings, api.RuntimeConfigurationChange{Action: "main", Selection: api.WorkExecutionSettings{Model: previous}, ExpectedRevision: baseline.Revision}); err != nil || result.Outcome != "committed" {
		t.Fatal("restore distinct Host default", err)
	}
	// The UI's Bot conversation setting patches its application session only;
	// provider routing resolves from that exact Host-advertised model selector.
	session := New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: previous, ApprovalMode: "workspace-write"}})
	if err = session.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{}, NotebookDirectory: root}); err != nil {
		t.Fatal(err)
	}
	if err = session.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	persisted := 0
	if err = session.ChangeExecution(ctx, api.ExecutionSettings{Model: selected, ApprovalMode: "workspace-write"}, func() error { persisted++; return nil }); err != nil {
		t.Fatal(err)
	}
	botConfig, err := session.Configuration(ctx)
	if err != nil || botConfig.Profile.Model != selected || persisted != 1 {
		t.Fatal("Bot selection did not retain its exact configured provider route", err)
	}
	unchanged, err := ReadRuntimeConfiguration(ctx, settings)
	if err != nil || unchanged.Main.Model != previous {
		t.Fatal("Bot-specific selection changed the Host global default", err)
	}
	view, err = ReadRuntimeConfiguration(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ChangeRuntimeConfiguration(ctx, settings, api.RuntimeConfigurationChange{Action: "main", Selection: api.WorkExecutionSettings{Model: selected}, ExpectedRevision: view.Revision}); err != nil || result.Outcome != "committed" {
		t.Fatal("Magpie route selection", err)
	}
	updated, err := ReadRuntimeConfiguration(ctx, settings)
	if err != nil || updated.Main.Model != selected {
		t.Fatal("selected native route was not readable after save", err)
	}
	routeFound := false
	for _, model := range updated.Models {
		if model.Model == selected && strings.Contains(strings.ToLower(model.Description), "magpie") {
			routeFound = true
		}
	}
	if !routeFound {
		t.Fatal("saved selector lost its Magpie provider route")
	}
	if os.Getenv("CAELIS_BOT_MAGPIE_CATALOG_URL") != "" {
		classifier := ""
		for _, m := range models {
			if m.Value == "typesafe/jev-latest" {
				classifier = m.Value
			}
		}
		if classifier == "" {
			t.Fatal("unified Magpie catalog lost Jev classifier")
		}
		flow, err := connections.Start(ctx, settings, api.RuntimeConnectionInput{Kind: "api-key", Choice: "magpie", BaseURL: baseURL, Model: classifier, APIKey: key})
		if err != nil {
			t.Fatal(err)
		}
		_ = waitConnection(t, connections, flow.ID, "complete")
		config, err := ReadRuntimeConfiguration(ctx, settings)
		if err != nil {
			t.Fatal(err)
		}
		profile := ""
		effort := ""
		for _, m := range config.Team.Models {
			if strings.Contains(m.Name, "jev-latest") {
				profile = m.Model
				effort = m.DefaultEffort
			}
		}
		if profile == "" {
			t.Fatal("saved classifier missing from Team model catalog")
		}
		listed := false
		for _, group := range config.Connections {
			for _, m := range group.Models {
				listed = listed || strings.Contains(m.Name, "jev-latest")
			}
		}
		if !listed {
			t.Fatal("saved classifier missing from connection management")
		}
		for _, m := range config.Models {
			if strings.Contains(m.Name, "jev-latest") {
				t.Fatal("classifier leaked into conversation choices")
			}
		}
		eligible := false
		for _, role := range config.Team.Roles {
			for _, id := range role.ModelIDs {
				if id != profile {
					continue
				}
				if role.ID == "guardian-screen" {
					eligible = true
				}
				if role.ID == "guardian" {
					t.Fatal("classifier incorrectly eligible as guardian LLM")
				}
			}
		}
		if !eligible {
			t.Fatal("Jev not eligible for guardian-screen")
		}
		result, err := ChangeRuntimeConfiguration(ctx, settings, api.RuntimeConfigurationChange{Action: "bind", ID: "guardian-screen", Selection: api.WorkExecutionSettings{Model: profile, Effort: effort}, ExpectedRevision: config.Revision})
		if err != nil || result.Outcome != "committed" {
			t.Fatal("classifier binding failed", result.Outcome, result.Message, err)
		}
		verified, err := ReadRuntimeConfiguration(ctx, settings)
		if err != nil {
			t.Fatal(err)
		}
		bound := false
		for _, role := range verified.Team.Roles {
			if role.ID == "guardian-screen" && role.Selection.Model == profile {
				bound = true
			}
		}
		if !bound {
			t.Fatal("classifier binding not retained")
		}
		t.Log("Jev discovered, connected, and bound to optional guardian-screen in temporary Host; no inference")
	}
	t.Logf("Bot Magpie catalog=%d connectable choices; selected Magpie provider route retained; application/global setting scopes verified; no inference", len(models))
}

func mustHostClient(t *testing.T, settings api.RuntimeSettings) *client {
	t.Helper()
	discovery, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	c, err := newClient(discovery.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.http.CloseIdleConnections)
	return c
}
