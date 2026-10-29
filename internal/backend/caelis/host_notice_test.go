package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
)

func TestSystemDeliveryRetriesOnlyUnwrittenNativeIntent(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/prompt") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		posts.Add(1)
		var req wire.PromptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "accepted"})
	})
	r, err := bot.NewForRuntime(filepath.Join(t.TempDir(), "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Start(s)
	defer r.Close()
	s.mu.Lock()
	path := s.path
	s.path = t.TempDir() // Native journal fails; resident delivery ledger stays writable.
	s.mu.Unlock()
	message := api.SystemMessage{ID: "original-notice", Source: api.SystemWorkerApproval, Text: "Original approval is pending."}
	receipt, err := r.SubmitSystem(t.Context(), message)
	if receipt.Outcome != "rejected" || !receipt.Deferred || posts.Load() != 0 {
		t.Fatalf("unwritten native intent became uncertain: %+v %v posts=%d", receipt, err, posts.Load())
	}
	s.mu.Lock()
	_, exists := s.state.Operations[message.ID]
	s.path = path
	s.mu.Unlock()
	if exists {
		t.Fatal("failed intent retained in native journal")
	}
	for range 2 {
		if receipt, err = r.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
			t.Fatal("original ID did not recover", receipt, err)
		}
	}
	if posts.Load() != 1 {
		t.Fatal("native POST repeated", posts.Load())
	}
}

func TestHostReportSubmissionIsHiddenThroughReplayAndReload(t *testing.T) {
	const notice = "Task task-42 is completed."
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		var req wire.PromptRequest
		if !strings.HasSuffix(r.URL.Path, "/sessions/main/prompt") || json.NewDecoder(r.Body).Decode(&req) != nil || value(req.SourceKind) != "external_material" || value(req.Input) != (api.SystemMessage{Source: api.SystemWorkerResult, Text: notice}).NativeInput().Text {
			t.Errorf("wrong host report submission: %s %+v", r.URL.Path, req)
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("host-turn")}})
	})
	in := api.Submission{ID: "host-report-42", Text: notice}
	if receipt, err := s.SubmitSystem(t.Context(), api.SystemMessage{Source: api.SystemWorkerResult, ID: in.ID, Text: in.Text, OriginRequests: in.OriginRequests}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Task task-42 is completed."}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"The requested work is ready."}}`)
	events := []wire.Envelope{
		{EventId: pointer("host-input"), TurnId: pointer("host-turn"), Update: &user},
		{EventId: pointer("host-reply"), TurnId: pointer("host-turn"), Update: &reply},
		{EventId: pointer("host-end"), Kind: "caelis/lifecycle", TurnId: pointer("host-turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
	}
	v := s.state.Views["main"]
	for _, event := range events {
		s.applyScheduledEnvelope(v, event, inputScope{})
	}
	got := s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "The requested work is ready." {
		t.Fatal("host input leaked or reply hidden", got.Items)
	}
	want := api.ReportSource{RuntimeOwner: got.RuntimeOwner, TurnKey: "host-turn", ReportID: in.ID, Phase: "completed"}
	if want.RuntimeOwner == "" || len(got.ReportSources) != 1 || got.ReportSources[0] != want {
		t.Fatal("private receipt association lost", got.ReportSources)
	}
	s.mu.Lock()
	cached := s.Snapshot()
	s.mu.Unlock()
	if len(cached.ReportSources) != 1 || cached.ReportSources[0] != want {
		t.Fatal("cached snapshot lost association")
	}
	cached.ReportSources[0].ReportID = "mutated"
	if s.Snapshot().ReportSources[0] != want {
		t.Fatal("cached source alias")
	}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" {
		t.Fatal("reload leaked host input", got.Items)
	}
	if len(got.ReportSources) != 1 || got.ReportSources[0] != want {
		t.Fatal("reload lost report association")
	}
	replay := &view{State: v.State, Seen: map[string]bool{}}
	for _, event := range events {
		s.applyScheduledEnvelope(replay, event, inputScope{})
	}
	s.state.Views["main"] = replay
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" {
		t.Fatal("canonical replay leaked host input", got.Items)
	}
	s.applyScheduledEnvelope(replay, wire.Envelope{EventId: pointer("human-input"), TurnId: pointer("host-turn"), InputOperationId: pointer("human-operation"), Update: &user}, inputScope{})
	got = s.Snapshot()
	if len(got.Items) != 2 || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal("same-text user input hidden", got.Items)
	}
}

func TestHostReportIsHiddenInOlderCaelisPage(t *testing.T) {
	const notice = "Task task-42 is completed."
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Task task-42 is completed."}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Reported result"}}`)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("history_before") != "older" {
			t.Errorf("wrong history request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, value any) {
			b, _ := json.Marshal(value)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{
			{EventId: pointer("old-host"), TurnId: pointer("host-turn"), Update: &user},
			{EventId: pointer("old-answer"), TurnId: pointer("host-turn"), Update: &reply},
			{EventId: pointer("old-human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user},
		}})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer("")})
	})
	s.state.Operations["host-report-42"] = journal{Path: "/sessions/main/prompt", Outcome: "accepted", TurnID: "host-turn", Source: wire.SessionSource{Kind: "application_summary"}}
	s.state.Views["main"].HistoryBefore = "older"
	if err := s.LoadEarlier(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := s.Snapshot()
	if len(got.Items) != 2 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "Reported result" || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal("older page leaked host report or hid ordinary content", got.Items)
	}
}

func TestLegacyReportRetryPreservesOriginalJournal(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			reads := 0
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/operations/legacy-report" {
					t.Errorf("legacy effect replayed: %s %s", r.Method, r.URL.Path)
					return
				}
				reads++
				writeFixture(w, wire.CommandResult{OperationId: "legacy-report", Outcome: "accepted"})
			})
			in := api.Submission{ID: "legacy-report", Text: "Original completion evidence"}
			source, sid := "application_summary", "main"
			body, _ := json.Marshal(wire.PromptRequest{OperationId: &in.ID, SessionId: &sid, Input: &in.Text, SourceKind: &source})
			original := journal{Path: "/sessions/main/prompt", Body: body, Digest: digest(body), Outcome: outcome, Source: wire.SessionSource{Kind: source, OperationId: in.ID}}
			s.state.Operations[in.ID] = original
			if err := s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			receipt, err := s.SubmitSystem(t.Context(), api.SystemMessage{Source: api.SystemWorkerResult, ID: in.ID, Text: in.Text, OriginRequests: in.OriginRequests})
			if err != nil || receipt.Outcome != outcome {
				t.Fatal("old report reclassified", receipt, err)
			}
			current := s.state.Operations[in.ID]
			if string(current.Body) != string(body) || current.Digest != original.Digest || current.Source.Kind != source || current.HostNotice {
				t.Fatal("old intent rewritten", current)
			}
			changed := in
			changed.Text = "Changed intent"
			if r, _ := s.SubmitSystem(t.Context(), api.SystemMessage{Source: api.SystemWorkerResult, ID: changed.ID, Text: changed.Text, OriginRequests: changed.OriginRequests}); r.Outcome != "rejected" || r.Message == "" || s.state.Operations[in.ID].Outcome != outcome {
				t.Fatal("old ID reused", r)
			}
			if err := s.recoverOperation(t.Context(), in.ID); err != nil {
				t.Fatal(err)
			}
			want := 0
			if outcome == "unknown" {
				want = 1
			}
			if reads != want || s.state.Operations[in.ID].Source.Kind != source {
				t.Fatal("legacy receipt was replayed or relabelled", reads)
			}
		})
	}
}

func TestOrdinaryEvidenceDoesNotInventCompletionReceipt(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	s.state.Operations["material"] = journal{Path: "/sessions/main/prompt", Outcome: "accepted", TurnID: "material-turn", Source: wire.SessionSource{Kind: "external_material", OperationId: "material"}}
	if out := s.Snapshot(); len(out.ReportSources) != 0 {
		t.Fatal("evidence kind became completion metadata", out.ReportSources)
	}
}

func TestSystemNoticeCannotEnterReplacementConversation(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Error("wrong-owner notice reached native transport") })
	message := api.SystemMessage{ID: "original-command-result", Source: api.SystemCommandResult, Text: "Original command completed.", RuntimeOwner: "retired-native-owner"}
	receipt, err := s.SubmitSystem(t.Context(), message)
	if err != nil || receipt.ID != message.ID || receipt.Outcome != "rejected" || !receipt.Deferred {
		t.Fatal("native target fence lost", receipt, err)
	}
	if s.SystemReceipt(message.ID).Outcome != "unknown" {
		t.Fatal("unattempted input gained a native receipt")
	}
}
