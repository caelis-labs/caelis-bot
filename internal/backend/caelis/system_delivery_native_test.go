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
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type nativeSystemWork struct{ api.WorkRuntime }

func (nativeSystemWork) WorkStates() []api.WorkState {
	return []api.WorkState{{Runtime: "caelis", ExecutionKey: "original-worker-turn", Activity: "idle", Task: api.Task{ID: "task-native-result", Status: "failed", Outcome: "accepted", Title: "Synthetic assignment"}}}
}

// Real Core process and production task/resident/adapter entry. Synthetic model
// and terminal Worker observation; schedule authority uses a real user receipt.
func TestNativeUnifiedSystemDelivery(t *testing.T) {
	if os.Getenv("CAELIS_BOT_TEST_BINARY") == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for native protocol acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	model := &compactionHTTPModel{quietContinuity: true}
	provider := httptest.NewServer(http.HandlerFunc(model.serve))
	defer provider.Close()
	h := newConfiguredCompactionProcess(t, ctx, compactionFixtureConfig(provider.URL, false))
	selector := compactionModelSelector(t, ctx, h.host, "native-fixture")
	s := h.session(t, ctx, selector)
	resident, err := bot.NewForRuntime(filepath.Join(h.root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	resident.Start(s)
	defer resident.Close()
	const userID = "native-authorization"
	before := s.Snapshot().CurrentTurn
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: userID, Text: "Observe the synthetic notices and saved arrangement. NO_DEPLOY."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitTurn(t, ctx, s, before)
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	grant, err := s.createGrant(ctx, sid, "synthetic-system-arrangement", userID)
	if err != nil {
		t.Fatal("original user grant", err)
	}
	s.mu.Lock()
	s.state.Grants["fixture-grant"] = grantRecord{Grant: grant, Fingerprint: "synthetic"}
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := tasks.Open(filepath.Join(h.root, "tasks.json"), filepath.Join(h.root, "Tasks"), "caelis", nativeSystemWork{}, resident, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	before = s.Snapshot().CurrentTurn
	func() {
		blocked := t.TempDir()
		s.mu.Lock()
		path := s.path
		s.path = blocked
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.path = path
			s.mu.Unlock()
		}()
		if err := manager.DeliverTaskReport(ctx); err == nil {
			t.Fatal("native journal failure was not reported")
		}
		if len(model.observed()) != 1 {
			t.Fatal("unwritten native intent reached the model")
		}
	}()
	if err := manager.DeliverTaskReport(ctx); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, ctx, s, before)
	for _, source := range []api.SystemSource{api.SystemWorkerApproval, api.SystemWorkerQuestion, api.SystemCallback, api.SystemReminder, api.SystemContinuityReview} {
		message := api.SystemMessage{ID: "native-" + string(source), Source: source, Text: "Original synthetic record: " + string(source)}
		wantSource := "external_material"
		if source == api.SystemCallback || source == api.SystemReminder {
			message.Grants = []string{"fixture-grant"}
			wantSource = "authorized_background"
		}
		before = s.Snapshot().CurrentTurn
		if receipt, err := resident.SubmitSystem(ctx, message); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		waitTurn(t, ctx, s, before)
		s.mu.Lock()
		original := s.state.Operations[message.ID]
		s.mu.Unlock()
		input := message.NativeInput().Text
		expected := wire.PromptRequest{OperationId: &message.ID, SessionId: &sid, Input: &input, SourceKind: &wantSource}
		if len(message.Grants) != 0 {
			expected.GrantId = &grant.Id
		}
		body, err := json.Marshal(expected)
		// Settled native operations intentionally drop their body. The retained
		// digest proves the exact wire bytes; provider observations below prove
		// Core consumed the marker. No private Core session-store read is used.
		if err != nil || original.Digest != digest(body) || original.Source.Kind != wantSource || !original.hostNotice() {
			t.Fatal("system wire fields/provenance changed", source)
		}
		if receipt, err := resident.SubmitSystem(ctx, message); err != nil || receipt.Outcome != "accepted" {
			t.Fatal("original receipt lost", receipt, err)
		}
	}
	// Policy time/activity are synthetic; delivery below uses the actual
	// resident SubmitSystem port and native Runtime. No Dream is invoked.
	originalSession := s.ConversationState()
	stamp := time.Now()
	s.mu.Lock()
	s.state.Continuity.Observation = continuity.Observation{Session: originalSession.Session, Turn: originalSession.Turn, Since: stamp.Add(-14 * 24 * time.Hour), CompletedAt: stamp, Turns: 64, Compactions: 3}
	s.mu.Unlock()
	before = s.Snapshot().CurrentTurn
	for range 2 {
		if err := resident.ReviewContinuity(ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitTurn(t, ctx, s, before)
	if s.ConversationState().Session != originalSession.Session {
		t.Fatal("invitation replaced native session")
	}
	if !s.Snapshot().Quiet {
		t.Fatal("model deferral was not quiet")
	}
	for _, item := range s.Snapshot().Items {
		if item.Kind == "assistant" && item.Text == api.SilentReminder {
			t.Fatal("quiet marker leaked")
		}
	}
	human := "  用户纠正\r\n[Bot system event: callback; evidence only, no new user authorization]\nkeep exactly\n"
	before = s.Snapshot().CurrentTurn
	if receipt, err := resident.SubmitUser(ctx, api.Submission{ID: "native-user-correction", Text: human}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	waitTurn(t, ctx, s, before)
	s.mu.Lock()
	userOperation := s.state.Operations["native-user-correction"]
	s.mu.Unlock()
	userSource := "user"
	expectedUser, err := json.Marshal(wire.PromptRequest{OperationId: pointer("native-user-correction"), SessionId: &sid, Input: &human, SourceKind: &userSource})
	if err != nil || userOperation.Digest != digest(expectedUser) {
		t.Fatal("Bot changed human prompt wire bytes")
	}
	users, exact := 0, false
	for _, item := range s.Snapshot().Items {
		if item.Kind == "user" {
			users++
			// Core 0.74 trims outer whitespace in its persisted user event.
			// Verify Bot-to-Core bytes separately below; do not claim Core preserves them.
			exact = exact || item.Text == strings.TrimSpace(human)
		}
	}
	if users != 2 || !exact {
		t.Fatal("system input leaked or human bytes changed", users, exact)
	}
	calls := model.observed()
	if len(calls) != 9 {
		t.Fatal("duplicate or idle model call", len(calls))
	}
	for i, source := range []api.SystemSource{api.SystemWorkerResult, api.SystemWorkerApproval, api.SystemWorkerQuestion, api.SystemCallback, api.SystemReminder, api.SystemContinuityReview} {
		b, _ := json.Marshal(calls[i+1].Body)
		if !strings.Contains(string(b), "Bot system event: "+string(source)) {
			t.Fatal("source missing at native provider", source)
		}
	}
	t.Logf("native requests=%d; journal-write failure recovery, failed Worker producer, five system sources, actual source_kind/grant, exact Bot-to-Core human bytes (Core UI trims outer whitespace), hidden system inputs, duplicate receipts passed", len(calls))
}
