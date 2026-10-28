package caelis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// These upstream regressions deliberately assert the desired contract. They
// fail on candidate 26865e53 and are separately opted in until Core fixes them.
// Bot must not add its own idle timer or rewrite provider accounting to mask them.
func requireCoreRegressionProbe(t *testing.T) {
	t.Helper()
	if os.Getenv("CAELIS_BOT_TEST_CORE_REGRESSIONS") != "1" {
		t.Skip("explicit upstream regression probe")
	}
}
func TestCoreStatusReadPreservesIdle(t *testing.T) {
	requireCoreRegressionProbe(t)
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
		})
	}
}
func TestCoreCompatibleCachedInputContext(t *testing.T) {
	requireCoreRegressionProbe(t)
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
	if value(warm.Usage.TotalTokens) < 34000 {
		t.Errorf("warm context omitted cached reads: total=%d, expected >=34000", value(warm.Usage.TotalTokens))
	}
	if value(warm.Usage.SessionUsageTotal.TotalTokens) != 68100 {
		t.Error("session accounting changed independently of context estimate")
	}
}
