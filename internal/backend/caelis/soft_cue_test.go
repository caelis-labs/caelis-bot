package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestContinuityProviderAccountingAndExclusions(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, basis, prompt, cached string
		want                        int64
	}{
		{"included", "included", "70000", "60000", 70000},
		{"separate", "separate", "10000", "60000", 70000},
		{"unknown basis", "", "70000", "0", 0},
		{"zero", "included", "0", "0", 0},
		{"missing", "included", "", "0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{tools: &api.ToolConnection{}}
			s.state.Session.SessionId = "resident"
			v := &view{Seen: map[string]bool{}}
			meta := wire.ACPMetadata{"caelis": map[string]any{"sdk": map[string]any{"provider": "fixture", "model": "model", "usage_receipt_id": "original-response", "usage": map[string]any{"cache_read_accounting": tc.basis, "prompt_tokens": tc.prompt, "cached_input_tokens": tc.cached, "cached_input_tokens_reported": true}}}}
			payload, _ := json.Marshal(wire.ACPUsageUpdate{SessionUpdate: "usage_update", Meta: &meta})
			update := wire.ACPUpdate(payload)
			e := wire.Envelope{EventId: pointer("original"), ProjectionId: pointer("projection"), SessionId: pointer("resident"), TurnId: pointer("turn"), Scope: pointer("main"), OccurredAt: &now, Update: &update, UsageSemantics: wire.UsageSemanticsProviderUsage}
			s.sampleContinuity(v, e, now)
			if s.state.Continuity.LatestInput != tc.want {
				t.Fatal(s.state.Continuity)
			}
			for _, kind := range []string{"child", "review", "worker", "replay", "gauge", "summary"} {
				before, _ := json.Marshal(s.state.Continuity)
				bad := e
				switch kind {
				case "child":
					bad.ParentTool = &wire.ParentToolRelation{}
				case "review":
					bad.ApprovalRequestId = pointer("review")
				case "worker":
					bad.SessionId = pointer("worker")
				case "replay":
					v.Seen["projection"] = true
				case "gauge":
					bad.UsageSemantics = wire.UsageSemanticsContextGauge
				case "summary":
					bad.Update = nil
				}
				s.sampleContinuity(v, bad, now)
				after, _ := json.Marshal(s.state.Continuity)
				delete(v.Seen, "projection")
				if string(before) != string(after) {
					t.Fatal("non-ordinary input sampled", kind)
				}
			}
		})
	}
}

func TestNativeContinuityProviderSampling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	model := &compactionHTTPModel{summaryStarted: make(chan struct{}, 1)}
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
	h := newConfiguredCompactionProcess(t, ctx, compactionFixtureConfig(server.URL, false))
	h.experiments(t, ctx, false, false)
	s := h.session(t, ctx, compactionModelSelector(t, ctx, h.host, "native-fixture"))
	for n := range 2 {
		prompt := fmt.Sprintf("Original prompt %d.\n  Preserve whitespace.  ", n)
		submitCompactionText(t, ctx, s, fmt.Sprintf("continuity-native-%d", n), prompt)
		s.mu.Lock()
		state := s.state.Continuity
		j := s.state.Operations[fmt.Sprintf("continuity-native-%d", n)]
		sid := s.state.Session.SessionId
		s.mu.Unlock()
		// Terminal journals compact Body but retain the exact dispatched digest.
		expected, err := json.Marshal(wire.PromptRequest{OperationId: pointer(fmt.Sprintf("continuity-native-%d", n)), SessionId: &sid, Input: &prompt, SourceKind: pointer("user")})
		if err != nil || j.Digest != digest(expected) {
			t.Fatal("sampling changed the original native request digest", err)
		}
		if state.LatestInput != 34000 || !state.Created.IsZero() || state.Since.IsZero() || state.Pending != nil {
			t.Fatalf("native accounting/age boundary: %+v", state)
		}
	}
	t.Log("native Anthropic cold/warm cache: full input 34000; age lower bound; original prompts unchanged; cue delivery disabled")
}
