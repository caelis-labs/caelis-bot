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
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

// Requires the separately built Core Draft binary at the exact contract SHA.
// Both the Host store and model endpoint are isolated under this test.
// Temporary combination acceptance: native protocols and file tools are real;
// age/activity and model choices are scripted, not autonomous-model evidence.
func TestNativeCombinedReview(t *testing.T) {
	for _, decision := range []string{"defer", "dream"} {
		t.Run(decision, func(t *testing.T) { nativeCombinedReview(t, decision == "dream") })
	}
}
func nativeCombinedReview(t *testing.T, dream bool) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to the Draft Core binary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	model := newAcceptanceModel()

	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	skillPath, err := botskills.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	memoryReference := filepath.Join(filepath.Dir(skillPath), "references", "memory.md")
	body, err := os.ReadFile(memoryReference)
	if err != nil {
		t.Fatal(err)
	}
	const guideMarker = "combined-review-reference-loaded"
	if err = os.WriteFile(memoryReference, append(body, []byte("\n"+guideMarker+"\n")...), 0600); err != nil {
		t.Fatal(err)
	}

	decision := modelStep{Reply: api.SilentReminder}
	if dream {
		decision = modelStep{Name: "bot_dream", Args: map[string]string{"handoff": "Identity: fixture Bot. Preference: short replies. Goal: original task; no deployment. Original task handle: task-123. Authorization: read only; deployment revoked. Unknown receipt: receipt-456; reconcile only. Review path: review-notes.md; reviewed src/a; unread src/b. Next: inspect original Worker question."}}
	}
	model.set("CASE_ORIGINAL", modelStep{Reply: "Original task retained."}, modelStep{Name: "Read", Args: map[string]string{"path": memoryReference}}, decision)
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
	if err := host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-responses", Model: "gpt-5.4-mini", ReasoningLevels: []string{"low", "medium", "high"}, ImageInput: pointer(true), BaseUrl: pointer(server.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &connected, op, string(status.Configuration.Revision)); err != nil || !succeeded(connected.Outcome) {
		t.Fatal("synthetic model setup", connected, err)
	}
	modelSelector := fixtureModelSelector(t, ctx, host, "gpt-5.4-mini")
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	if err = os.WriteFile(filepath.Join(vault.Path(), "MEMORY.md"), []byte("Identity: fixture Bot. Stable preference: short replies. Standing constraint: ask before publishing.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureDream(vault, ""); err != nil {
		t.Fatal(err)
	}
	config := &api.ToolConnection{Host: resident, ApprovedTools: []string{"bot_dream"}, NotebookDirectory: vault.Path(), Instructions: botskills.Bootstrap(), SkillInstructions: botskills.Instructions(skillPath), BuiltinSkillRoots: []string{filepath.Dir(skillPath)}}
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
	s := New(Options{Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: modelSelector, Effort: "low", ApprovalMode: "workspace-write"}})
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
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "case_original", Text: "CASE_ORIGINAL continue within read-only authorization."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}

	waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_ORIGINAL")) == 1 && s.Snapshot().CanSend })
	stamp, observed := time.Now(), s.ConversationState()
	s.mu.Lock()
	s.state.Continuity.Observation = continuity.Observation{Session: observed.Session, Turn: observed.Turn, Since: stamp.Add(-14 * 24 * time.Hour), CompletedAt: stamp, Turns: 64, Compactions: 3, Maintenance: [8]continuity.MaintenanceTurn{{Turn: 62, At: stamp}, {Turn: 63, At: stamp}, {Turn: 64, At: stamp}}}
	s.mu.Unlock()
	for range 2 {
		if err := resident.ReviewContinuity(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !dream {
		waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_ORIGINAL")) == 3 && s.Snapshot().CanSend })
		if !s.Snapshot().Quiet || s.ConversationState().Session != old || resident.PrepareHandoffContext().HandoffDigest != "" {
			t.Fatal("healthy deferral renewed or leaked quiet result")
		}
		requests := model.seen("CASE_ORIGINAL")
		beforeRead, _ := json.Marshal(requests[1])
		afterRead, _ := json.Marshal(requests[2])
		if strings.Contains(string(beforeRead), guideMarker) || !strings.Contains(string(afterRead), guideMarker) || !strings.Contains(string(beforeRead), "Bot system event: continuity_review") {
			t.Fatal("native reference read or invitation missing")
		}
		if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "healthy-next", Text: "CASE_HEALTHY keep working; no broader authorization."}, nil); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_HEALTHY")) == 1 && s.Snapshot().CanSend })
		stamp, observed = time.Now(), s.ConversationState()
		s.mu.Lock()
		s.state.Continuity.Observation = continuity.Observation{Session: observed.Session, Turn: observed.Turn, Since: stamp.Add(-14 * 24 * time.Hour), CompletedAt: stamp, Turns: 128, Compactions: 6, Maintenance: [8]continuity.MaintenanceTurn{{Turn: 126, At: stamp}, {Turn: 127, At: stamp}, {Turn: 128, At: stamp}}}
		s.mu.Unlock()
		resident.Close()
		reopened, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
		if err != nil {
			t.Fatal(err)
		}
		reopened.Start(s)
		defer reopened.Close()
		for range 2 {
			if err := reopened.ReviewContinuity(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if len(model.seen("CASE_HEALTHY")) != 1 {
			t.Fatal("restart repeated invitation")
		}
		t.Log("native reference read -> quiet deferral -> ordinary work; restart cooldown preserved; no Dream")
		return
	}
	waitAcceptance(t, ctx, func() bool {
		state := s.ConversationState()
		return state.Session != "" && state.Session != old && resident.PrepareHandoffContext().HandoffDigest != ""
	})
	if s.BotPluginGeneration() == oldGeneration {
		t.Fatal("new Core Session retained old tool discovery generation")
	}
	if len(model.seen("CASE_ORIGINAL")) != 3 {
		t.Fatal("old Core Turn entered a second model step")
	}
	requests := model.seen("CASE_ORIGINAL")
	beforeRead, _ := json.Marshal(requests[1])
	afterRead, _ := json.Marshal(requests[2])
	if strings.Contains(string(beforeRead), guideMarker) || !strings.Contains(string(afterRead), guideMarker) || !strings.Contains(string(beforeRead), "Bot system event: continuity_review") {
		t.Fatal("Dream did not follow native reference read")
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
	if receipt, err := resident.SubmitSystem(ctx, api.SystemMessage{ID: "case_next", Source: api.SystemWorkerQuestion, Text: "CASE_NEXT original Worker question awaits a response."}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(model.seen("CASE_NEXT")) != 0 })
	first, _ := json.Marshal(model.seen("CASE_NEXT")[0])
	if !strings.Contains(string(first), "receipt-456") || strings.Contains(string(first), originalCall) {
		t.Fatal("new first prompt lost private handoff or inherited old call", string(first))
	}
	for _, fact := range []string{"task-123", "read only", "deployment revoked", "receipt-456", "review-notes.md", "unread src/b"} {
		if !strings.Contains(string(first), fact) {
			t.Fatal("handoff lost supplied fact", fact)
		}
	}
	t.Log("native reminder -> reference read -> Dream -> Worker question first seed; supplied task/auth/review facts retained")
	if len(s.state.PastSessions) == 0 || s.state.PastSessions[len(s.state.PastSessions)-1] != old {
		t.Fatal("old Core Session identity not retained")
	}

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
