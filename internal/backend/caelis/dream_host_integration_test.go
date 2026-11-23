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
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// Both the Host store and model endpoint are isolated under this test.
func TestNativeHostBotDreamRenewal(t *testing.T) {
	ctx, s, resident, model := nativeDreamFixture(t, nil)
	oldModelEntered, release := make(chan struct{}, 1), make(chan struct{})
	finishOld := sync.OnceFunc(func() { close(release) })
	defer finishOld()
	model.set("CASE_DREAM",
		modelStep{Name: "bot_dream", Args: map[string]string{"handoff": "Identity: fixture Bot. Original task task-123; uncertain receipt receipt-456. Continue on the next user request."}},
		modelStep{Entered: oldModelEntered, Block: release, Reply: "Original conversation finished late."},
	)
	old := s.ConversationState().Session
	oldGeneration := s.BotPluginGeneration()
	initial := api.Submission{ID: "case_dream", Text: "CASE_DREAM"}
	if receipt, err := resident.SubmitUser(ctx, initial, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	select {
	case <-oldModelEntered:
	case <-ctx.Done():
		t.Fatal("old turn did not continue naturally", ctx.Err())
	}
	waitAcceptance(t, ctx, func() bool {
		state := s.ConversationState()
		return state.Session != "" && state.Session != old && resident.PrepareHandoffContext().HandoffDigest != ""
	})
	if s.BotPluginGeneration() == oldGeneration {
		t.Fatal("new Core Session retained old tool discovery generation")
	}
	var original wire.SessionToolCall
	s.mu.Lock()
	for id, record := range s.state.Calls {
		if record.Call.Name == "bot_dream" {
			if record.Phase != "completed" || record.Receipt == nil || record.Receipt.Outcome != "succeeded" {
				s.mu.Unlock()
				t.Fatal("original terminal callback receipt missing", record)
			}
			original = record.Call
			if id != original.Id || s.state.RenewedBy != id || !value(s.state.Views[old].State.Run.Active) {
				s.mu.Unlock()
				t.Fatal("handoff waited for old turn or lost original identity")
			}
		}
	}
	s.mu.Unlock()
	if original.Id == "" {
		t.Fatal("registered tool did not produce an original callback")
	}
	invocation := api.ToolInvocation{Provider: "caelis", CallID: original.Id, Session: old, Turn: original.TurnId}
	if session, err := s.RenewAfterTool(ctx, invocation); err != nil || session != s.ConversationState().Session {
		t.Fatal("duplicate handoff did not return its original destination", session, err)
	}
	if receipt, err := resident.SubmitUser(ctx, initial, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("duplicate source message lost its receipt", receipt, err)
	}
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_next", Text: "CASE_NEXT continue the original task."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_NEXT")) != 0 })
	first, _ := json.Marshal(model.seen("CASE_NEXT")[0])
	if !strings.Contains(string(first), "receipt-456") || strings.Contains(string(first), original.Id) || strings.Contains(string(first), "CASE_DREAM") {
		t.Fatal("new first prompt lost private handoff or inherited old call", string(first))
	}
	s.mu.Lock()
	oldStillRunning := value(s.state.Views[old].State.Run.Active)
	owned := s.state.Operations["case_next"].Path == "/sessions/"+idPath(s.state.Session.SessionId)+"/prompt"
	s.mu.Unlock()
	if !oldStillRunning || !owned {
		t.Fatal("next message did not run independently in the new session")
	}
	finishOld()
	waitAcceptance(t, ctx, func() bool {
		for _, item := range s.Snapshot().Items {
			if item.Text == "Original conversation finished late." && item.TurnKey == original.TurnId && item.Status == "completed" {
				return true
			}
		}
		return false
	})

	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_later", Text: "CASE_LATER keep the original limits."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_LATER")) != 0 && s.Snapshot().CanSend })
	later, _ := json.Marshal(model.seen("CASE_LATER")[0])
	if strings.Count(string(later), "[Private Bot handoff:") != 1 || strings.Count(string(later), "[Bot context:") != 1 || resident.PrepareHandoffContext().HandoffDigest != "" {
		t.Fatal("native Core reinjected or lost the accepted seed")
	}
}

func nativeDreamFixture(t *testing.T, beforeConnect func(api.RuntimeSettings), configureResident ...func(*bot.Runtime)) (context.Context, *Session, *bot.Runtime, *acceptanceModel) {
	t.Helper()
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for isolated external Host acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	model := newAcceptanceModel()
	model.set("CASE_DREAM", modelStep{Name: "bot_dream", Args: map[string]string{"handoff": "Identity: fixture Bot. Goal: continue original task. Original task handle: task-123. Unknown receipt: receipt-456. Next: respond to the next user input."}})
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
	op := "dream-fixture-model"
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
	for _, configure := range configureResident {
		configure(resident)
	}
	config := &api.ToolConnection{Host: resident, ApprovedTools: []string{"bot_dream"}, NotebookDirectory: vault.Path(), Instructions: "Fixture Bot. Use only requested tool calls."}
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
	if beforeConnect != nil {
		beforeConnect(settings)
	}
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
