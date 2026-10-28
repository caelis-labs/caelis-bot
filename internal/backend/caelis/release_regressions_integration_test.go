package caelis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Core v0.74.0 fixes both candidate regressions. These process assertions run
// whenever CAELIS_BOT_TEST_BINARY is supplied, without an extra opt-in gate.
// Bot must not own another idle timer or rewrite provider accounting.
func TestCoreStatusReadPreservesIdle(t *testing.T) {
	for _, diagnostics := range []bool{false, true} {
		name := "plain"
		if diagnostics {
			name = "diagnostics"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
			defer cancel()
			model := &compactionHTTPModel{summaryStarted: make(chan struct{}, 1)}
			server := httptest.NewServer(http.HandlerFunc(model.serve))
			defer server.Close()
			h := newConfiguredCompactionProcess(t, ctx, compactionFixtureConfig(server.URL, false))
			h.experiments(t, ctx, false, true)
			s := h.session(t, ctx, compactionModelSelector(t, ctx, h.host, "native-fixture"))
			seedCompactionTurns(t, ctx, s, h.root, false)
			path := "/sessions/" + idPath(s.ConversationState().Session) + "/status"
			if diagnostics {
				path += "?diagnostics=true"
			}
			var status any
			if err := s.client.json(ctx, "GET", path, nil, &status, "", ""); err != nil {
				t.Fatal(err)
			}
			defer func() { writeCompactionEvidence(t, map[string]any{"status": status, "requests": model.observed()}) }()
			select {
			case <-model.summaryStarted:
			case <-time.After(65 * time.Second):
				t.Fatal("read-only status cancelled the eligible idle opportunity; no summary after 65 seconds")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			waitAcceptance(t, ctx, func() bool { return fixtureCheckpointCount(h.settings.CaelisStore) == 1 })
			t.Logf("status read preserved idle through canonical checkpoint; requests=%d", len(model.observed()))
		})
	}
}
func TestCoreCompatibleCachedInputContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	model := &compactionHTTPModel{}
	server := httptest.NewServer(http.HandlerFunc(model.serve))
	defer server.Close()
	h := newCompactionProcess(t, ctx)
	selector := h.connectModel(t, ctx, "custom-messages", "claude-fixture", server.URL)
	h.experiments(t, ctx, false, false)
	s := h.session(t, ctx, selector)
	submitCompactionText(t, ctx, s, "cold", "Synthetic cold usage counter probe.")
	cold := compactionStatus(t, ctx, s)
	submitCompactionText(t, ctx, s, "warm", "Synthetic warm usage counter probe.")
	warm := compactionStatus(t, ctx, s)
	writeCompactionEvidence(t, map[string]any{"cold": cold.Usage, "warm": warm.Usage, "requests": model.observed()})
	if value(warm.Usage.TotalTokens) != 34016 {
		t.Errorf("warm context accounting: total=%d, expected 34016", value(warm.Usage.TotalTokens))
	}
	if value(warm.Usage.SessionUsageTotal.TotalTokens) != 68100 {
		t.Error("session accounting changed independently of context estimate")
	}
	t.Logf("warm context=%d session total=%d", value(warm.Usage.TotalTokens), value(warm.Usage.SessionUsageTotal.TotalTokens))
}
