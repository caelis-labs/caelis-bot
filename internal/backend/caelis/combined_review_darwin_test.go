package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to the Draft Core binary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	model := newAcceptanceModel()

	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer closeNativeHTTP(server)
	hostFixture := newNativeHostFixture(t, bin)
	root, settings := hostFixture.Root, hostFixture.settings

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

	steps := []modelStep{
		{Reply: "Original task retained."},
		{Name: "Read", Args: map[string]string{"path": memoryReference}},
		{Reply: api.SilentReminder},
	}
	model.set("CASE_ORIGINAL", steps...)
	environment := newExecutionFixture(t, root)
	hostFixture.Env = environment.env
	hostFixture.start(t, ctx)
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
	config := &api.ToolConnection{NotebookDirectory: vault.Path(), Instructions: botskills.Bootstrap(), SkillInstructions: botskills.Instructions(skillPath), BuiltinSkillRoots: []string{filepath.Dir(skillPath)}}
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
	defer closeNativeFixture(t, s)
	resident.Start(s)
	defer resident.Stop()
	if err := s.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	old := s.ConversationState().Session
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
}
