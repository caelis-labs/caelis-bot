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
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// Requires the separately built Core Draft binary at the exact contract SHA.
// Both the Host store and model endpoint are isolated under this test.
func TestNativeHostBotDreamRenewal(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to the Draft Core binary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	model := newAcceptanceModel()
	model.set("CASE_DREAM", modelStep{Name: "bot_dream", Args: map[string]string{"handoff": "Identity: fixture Bot. Goal: continue original task. Original task handle: task-123. Unknown receipt: receipt-456. Next: respond to the next user input."}})
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
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
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
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
	op := "dream-fixture-model"
	var connected wire.CommandResult
	if err := host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "openai", Model: "gpt-5.4-mini", BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &connected, op, string(status.Configuration.Revision)); err != nil || !succeeded(connected.Outcome) {
		t.Fatal("synthetic model setup", connected, err)
	}
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureDream(vault, ""); err != nil {
		t.Fatal(err)
	}
	config := &api.ToolConnection{Host: resident, ApprovedTools: []string{"bot_dream"}, NotebookDirectory: vault.Path(), Instructions: "Fixture Bot. Use only requested tool calls."}
	config.PrepareContext = func(ctx context.Context) (api.ContextSeed, error) {
		seed, err := vault.PrepareContext(ctx)
		if err != nil {
			return seed, err
		}
		handoff := resident.PrepareHandoffContext()
		seed.Text += handoff.Text
		seed.HandoffDigest = handoff.HandoffDigest
		return seed, nil
	}
	config.ConsumeContext = resident.ConsumeHandoffContext
	config.FinishTurn = func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = resident.RenewPendingToolHandoff(ctx)
		}()
	}
	s := New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: "openai/gpt-5.4-mini", Effort: "low", ApprovalMode: "workspace-write"}})
	resident.Start(s)
	defer resident.Stop()
	if err := s.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	old := s.ConversationState().Session
	oldGeneration := s.BotPluginGeneration()
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_dream", Text: "CASE_DREAM"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitAcceptance(t, ctx, func() bool {
		state := s.ConversationState()
		return state.Session != "" && state.Session != old && resident.PrepareHandoffContext().HandoffDigest != ""
	})
	if s.BotPluginGeneration() == oldGeneration {
		t.Fatal("new Core Session retained old tool discovery generation")
	}
	if len(model.seen("CASE_DREAM")) != 1 {
		t.Fatal("old Core Turn entered a second model step")
	}
	var originalCall string
	for id, record := range s.state.Calls {
		if record.Call.Name == "bot_dream" {
			originalCall = id
			if record.Receipt == nil || !value(record.Receipt.TurnComplete) || record.Receipt.Outcome != "succeeded" {
				t.Fatal("original terminal callback receipt missing", record)
			}
		}
	}
	if originalCall == "" || s.state.RenewedBy != originalCall {
		t.Fatal("new session not bound to original callback ID", originalCall, s.state.RenewedBy)
	}
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_next", Text: "CASE_NEXT continue."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_NEXT")) != 0 })
	first, _ := json.Marshal(model.seen("CASE_NEXT")[0])
	if !strings.Contains(string(first), "receipt-456") || strings.Contains(string(first), originalCall) {
		t.Fatal("new first prompt lost private handoff or inherited old call", string(first))
	}
	if len(s.state.PastSessions) == 0 || s.state.PastSessions[len(s.state.PastSessions)-1] != old {
		t.Fatal("old Core Session identity not retained")
	}
}
