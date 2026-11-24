package caelis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func nativeResidentFixture(t *testing.T, configureResident func(*bot.Runtime)) (context.Context, *Session, *bot.Runtime, *acceptanceModel) {
	t.Helper()
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	model := newAcceptanceModel()
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	t.Cleanup(server.Close)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	if err := os.Mkdir(filepath.Join(root, "home"), 0700); err != nil {
		t.Fatal(err)
	}
	environment := newExecutionFixture(t, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, environment.env
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	t.Cleanup(func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() })
	waitAcceptance(t, ctx, func() bool { _, _, err := Discover(settings); return err == nil })
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	var status wire.StatusSnapshot
	if err := host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "resident-fixture-model"
	var connected wire.CommandResult
	if err := host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-responses", Model: "gpt-5.4-mini", ReasoningLevels: []string{"low", "medium", "high"}, ImageInput: pointer(true), BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &connected, op, string(status.Configuration.Revision)); err != nil || !succeeded(connected.Outcome) {
		t.Fatal("synthetic model setup", connected, err)
	}
	modelSelector := fixtureModelSelector(t, ctx, host, "gpt-5.4-mini")
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureDream(vault, ""); err != nil {
		t.Fatal(err)
	}
	configureResident(resident)
	config := &api.ToolConnection{NotebookDirectory: vault.Path(), Instructions: "Fixture Bot. Use only requested tool calls."}
	config.PrepareContext = func(ctx context.Context) (api.ContextSeed, error) {
		seed, err := vault.PrepareContext(ctx)
		if err != nil {
			return seed, err
		}
		handoff := resident.PrepareHandoffContextFor(api.ContextSession(ctx))
		seed.Text += handoff.Text
		seed.HandoffDigest = handoff.HandoffDigest
		return seed, nil
	}
	config.ConsumeContext = resident.ConsumeHandoffContext
	s := New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: modelSelector, Effort: "low", ApprovalMode: "workspace-write"}})
	resident.Start(s)
	t.Cleanup(resident.Stop)
	if err := s.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	return ctx, s, resident, model
}
