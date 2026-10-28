package caelis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCoreIdleForegroundAndRestart(t *testing.T) {
	for _, phase := range []string{"timer", "generation", "close_restart"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
			defer cancel()
			release := make(chan struct{})
			model := &compactionHTTPModel{summaryStarted: make(chan struct{}, 1), summaryRelease: release}
			server := httptest.NewServer(http.HandlerFunc(model.serve))
			defer server.Close()
			h := newConfiguredCompactionProcess(t, ctx, compactionFixtureConfig(server.URL, false))
			h.experiments(t, ctx, false, true)
			selector := compactionModelSelector(t, ctx, h.host, "native-fixture")
			s := h.session(t, ctx, selector)
			seedCompactionTurns(t, ctx, s, h.root, false)
			if phase == "timer" {
				select {
				case <-time.After(8 * time.Second):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				submitCompactionText(t, ctx, s, "foreground", "Foreground takes priority; retain the original goal.")
				select {
				case <-model.summaryStarted:
					t.Fatal("old idle timer survived foreground")
				case <-time.After(55 * time.Second):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			} else {
				select {
				case <-model.summaryStarted:
				case <-ctx.Done():
					t.Fatal("idle generation missing")
				}
				if phase == "generation" {
					submitCompactionText(t, ctx, s, "foreground", "Foreground interrupts summary generation; retain original receipts.")
				} else {
					previous := s.Snapshot().CurrentTurn
					h.stop()
					_ = s.Close(context.Background())
					h.start(t, ctx)
					restored := h.session(t, ctx, selector)
					if restored.Snapshot().CurrentTurn != previous {
						t.Fatal("restart replaced original foreground identity")
					}
					select {
					case <-time.After(3 * time.Second):
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				close(release)
			}
			if fixtureCheckpointCount(h.settings.CaelisStore) != 0 {
				t.Fatal("cancelled producer committed a checkpoint")
			}
			calls := model.observed()
			want := 4
			if phase == "timer" || phase == "close_restart" {
				want = 3
			}
			if len(calls) != want {
				t.Fatalf("unexpected replay or missing foreground: requests=%d want=%d", len(calls), want)
			}
			writeCompactionEvidence(t, map[string]any{"phase": phase, "requests": calls, "checkpointCount": 0})
		})
	}
}
