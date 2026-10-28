package caelis

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in to an already authorized local endpoint. Use direct access:
// Magpie correctly rejects unauthenticated HTTP forwarding proxies. This probe
// never reads conversations, changes user defaults, or disables that boundary.
// Three synthetic foreground turns, one native idle job, a three-minute deadline
// and 512 output tokens per request bound this acceptance exercise. Core retains
// its native retry policy; there is no test-level retry or request replay.
func TestCoreRealProviderCacheAndSummary(t *testing.T) {
	base := os.Getenv("CAELIS_BOT_TEST_MAGPIE_URL")
	if base == "" {
		t.Skip("set CAELIS_BOT_TEST_MAGPIE_URL for real-provider acceptance")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("requires explicitly selected local Magpie endpoint")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	config := map[string]any{"models": map[string]any{"configs": []any{map[string]any{
		"alias": "real-cache-probe", "provider": "magpie", "api": "openai_responses", "model": "deepseek/deepseek-flash", "base_url": base, "auth_type": "none", "context_window_tokens": 64000, "max_output_tokens": 512, "reasoning_effort": "none", "reasoning_levels": []string{"none"},
	}}}}
	h := newConfiguredCompactionProcess(t, ctx, config)
	h.experiments(t, ctx, false, true)
	s := h.session(t, ctx, compactionModelSelector(t, ctx, h.host, "real-cache-probe"))
	goal := "Entirely synthetic acceptance. Goal: prepare the synthetic release checklist. Constraints: NO_DEPLOY; do not change the default model; do not use tools. Pending: verify checksum and obtain owner review. Receipt SYNTHETIC_UPLOAD is UNKNOWN: do not replay it. Acknowledge in one short sentence.\n"
	submitCompactionText(t, ctx, s, "real-cold", goal+strings.Repeat("Synthetic archived context evidence 0123456789. ", 3500))
	first := compactionStatus(t, ctx, s) // One turn is not idle-eligible yet.
	submitCompactionText(t, ctx, s, "real-warm", "Keep the synthetic goal, constraints, pending work and UNKNOWN receipt unchanged. Acknowledge in one short sentence; no tools.")
	started := time.Now()
	waitAcceptance(t, ctx, func() bool { return fixtureCheckpointCount(h.settings.CaelisStore) == 1 })
	elapsed := time.Since(started)
	checkpoint := fixtureCheckpointText(t, h.settings.CaelisStore)
	for _, term := range []string{"release", "NO_DEPLOY", "default model", "checksum", "review", "SYNTHETIC_UPLOAD", "UNKNOWN"} {
		if !strings.Contains(strings.ToLower(checkpoint), strings.ToLower(term)) {
			t.Errorf("real summary omitted %q", term)
		}
	}
	afterSummary := compactionStatus(t, ctx, s)
	if value(afterSummary.Usage.SessionUsageTotal.CachedInputTokens) <= 0 || !value(afterSummary.Usage.SessionUsageTotal.CachedInputTokensReported) {
		t.Error("no observed real cache read")
	}
	submitCompactionText(t, ctx, s, "real-resume", "In one short paragraph state the original goal, limits, pending checks and unknown receipt. Do not use tools or perform any work.")
	final := compactionStatus(t, ctx, s)
	writeCompactionEvidence(t, map[string]any{"provider": "real Magpie deepseek/deepseek-flash", "first": first.Usage, "afterSummary": afterSummary.Usage, "final": final.Usage, "checkpoint": checkpoint, "idleElapsed": elapsed.String()})
	t.Logf("real provider checkpoint adopted, idle=%s cached_input=%d reported=%t", elapsed, value(final.Usage.SessionUsageTotal.CachedInputTokens), value(final.Usage.SessionUsageTotal.CachedInputTokensReported))
}
func fixtureCheckpointText(t *testing.T, store string) string {
	t.Helper()
	var result string
	err := filepath.WalkDir(filepath.Join(store, "sessions"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(path, ".events.jsonl") {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var event map[string]any
			if json.Unmarshal([]byte(line), &event) == nil && event["type"] == "compact" {
				result = line
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
