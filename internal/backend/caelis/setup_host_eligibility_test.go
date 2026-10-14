package caelis

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Opt-in cross-repository contract check. The CLI must be built from the
// reviewed Core SHA; it runs with a temporary Store and synthetic gateway.
// No inference request, account or production Host is involved.
func TestExternalHostSetupCatalogEligibility(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_ELIGIBILITY_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_ELIGIBILITY_BINARY to an isolated Core CLI")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	fixture := newExecutionFixture(t, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, fixture.env
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	waitAcceptance(t, ctx, func() bool { _, _, err := Discover(settings); return err == nil })

	endpoints, err := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "endpoints", Provider: "deepseek"})
	if err != nil || len(endpoints) == 0 || endpoints[0].Value == "" {
		t.Fatalf("Core returned no DeepSeek endpoint: %+v, %v", endpoints, err)
	}
	baseURL := endpoints[0].Value
	host, err := setupClient(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer host.http.CloseIdleConnections()
	payload, _ := json.Marshal(map[string]string{"provider": "deepseek", "base_url": baseURL})
	command := "connect-model:" + url.QueryEscape(string(payload))
	var raw []map[string]any
	if err := host.json(ctx, "POST", "/completion/slash-arguments", wire.CompletionRequest{Command: &command, Limit: pointer(1000)}, &raw, "", ""); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("Core returned no DeepSeek model directory")
	}
	for _, row := range raw {
		if row["chat_eligibility"] != "chat" || row["model_metadata_complete"] != true {
			t.Fatalf("Core wire candidate lacks complete chat facts: %+v", row)
		}
	}
	deepseek, err := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "models", Provider: "deepseek", BaseURL: baseURL})
	if err != nil || len(deepseek) != len(raw) {
		t.Fatalf("Bot lost Core DeepSeek directory: %+v, %v", deepseek, err)
	}
	for _, row := range deepseek {
		if row.ChatEligibility != "chat" || row.ModelMetadataComplete == nil || !*row.ModelMetadataComplete {
			t.Fatalf("Bot lost complete chat eligibility: %+v", row)
		}
	}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected gateway request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeFixture(w, map[string]any{"data": []map[string]any{
			{"id": "vendor/chat", "kind": "chat", "context_window": 32000, "max_output_tokens": 4096, "reasoning": false, "modalities": map[string]any{"input": []string{"text"}}},
			{"id": "vendor/unknown", "type": "model"},
			{"id": "xiaomi/mimo-v2.5-asr", "type": "model"},
			{"id": "typesafe/jev", "kind": "decision"},
		}})
	}))
	defer gateway.Close()
	magpie, err := SetupCatalog(ctx, api.SetupRequest{Settings: settings, Action: "models", Provider: "magpie", BaseURL: gateway.URL + "/v1"})
	if err != nil || len(magpie) != 2 {
		t.Fatalf("Bot Magpie chat catalog = %+v, %v", magpie, err)
	}
	kinds := map[string]string{}
	for _, row := range magpie {
		kinds[row.Value] = row.ChatEligibility
	}
	if kinds["vendor/chat"] != "chat" || kinds["vendor/unknown"] != "unknown" || kinds["xiaomi/mimo-v2.5-asr"] != "" || kinds["typesafe/jev"] != "" {
		t.Fatalf("Bot Magpie qualification = %+v", kinds)
	}
	// Registration against the synthetic gateway never sends a model request.
	// A committed unknown model must not make first-run chat ready; the
	// catalog-backed chat model must retain its evidence after configuration.
	connections := &Connections{}
	defer connections.Close()
	for _, model := range []struct {
		name, want      string
		context, output int
	}{
		{name: "vendor/unknown", want: "models", context: 32000, output: 4096},
		{name: "vendor/chat", want: "ready"},
	} {
		flow, err := connections.Start(ctx, settings, api.RuntimeConnectionInput{Kind: "api-key", Choice: "magpie", BaseURL: gateway.URL + "/v1", Model: model.name, APIKey: "SYNTHETIC_ONLY", ContextWindowTokens: model.context, MaxOutputTokens: model.output})
		if err != nil {
			t.Fatalf("register %s: %v", model.name, err)
		}
		_ = waitConnection(t, connections, flow.ID, "complete")
		state, err := InspectSetup(ctx, settings)
		if err != nil || state.State != model.want {
			t.Fatalf("registered %s: state=%+v err=%v", model.name, state, err)
		}
		found := false
		for _, configured := range state.Models {
			if configured.ChatEligibility == kinds[model.name] {
				found = true
			}
		}
		if !found {
			t.Fatalf("configured %s lost Core eligibility: %+v", model.name, state.Models)
		}
	}
	if output := os.Getenv("CAELIS_BOT_ELIGIBILITY_OUTPUT"); output != "" {
		data, err := json.Marshal(map[string]any{"endpoints": endpoints, "coreWire": raw, "deepseek": deepseek, "magpie": magpie})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
