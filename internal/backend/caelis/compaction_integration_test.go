package caelis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
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
)

const syntheticHandoff = "# Goal\nPrepare the synthetic release checklist.\n# Constraints\nNO_DEPLOY. Do not change the default model. Preserve original receipts.\n# Pending\nVerify the artifact checksum, then ask the owner to review. No deployment is authorized."

type compactionHTTPCall struct {
	Body    map[string]any
	Started time.Time
	Summary bool
}
type compactionHTTPModel struct {
	mu             sync.Mutex
	calls          []compactionHTTPCall
	summaryStarted chan struct{}
	summaryRelease <-chan struct{}
	toolFirst      bool
}

func (m *compactionHTTPModel) observed() []compactionHTTPCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]compactionHTTPCall(nil), m.calls...)
}
func (m *compactionHTTPModel) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		http.Error(w, "fixture body", 400)
		return
	}
	summary := strings.Contains(string(raw), "Internal compaction for this ongoing task.")
	m.mu.Lock()
	n := len(m.calls)
	m.calls = append(m.calls, compactionHTTPCall{Body: body, Started: time.Now(), Summary: summary})
	release := m.summaryRelease
	m.mu.Unlock()
	if summary && m.summaryStarted != nil {
		select {
		case m.summaryStarted <- struct{}{}:
		default:
		}
	}
	if summary && release != nil {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	reply := "SYNTHETIC_ACK: goal, NO_DEPLOY and pending checksum retained."
	if summary {
		reply = syntheticHandoff
	}
	usage := map[string]any{"input_tokens": 9000, "output_tokens": 0, "cache_read_input_tokens": 25000, "cache_creation_input_tokens": 0, "cache_creation": map[string]any{"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 0}}
	if n == 0 {
		usage["cache_read_input_tokens"] = 0
		usage["cache_creation_input_tokens"] = 25000
		usage["cache_creation"] = map[string]any{"ephemeral_5m_input_tokens": 25000, "ephemeral_1h_input_tokens": 0}
	}
	id := fmt.Sprintf("synthetic-%d", n)
	if _, responses := body["input"]; responses {
		w.Header().Set("Content-Type", "text/event-stream")
		response := map[string]any{"model": body["model"], "status": "completed", "output": []any{map[string]any{"id": id, "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": reply}}}}, "usage": map[string]any{"input_tokens": 34000, "output_tokens": 50, "total_tokens": 34050, "input_tokens_details": map[string]any{"cached_tokens": 25000}}}
		for _, event := range []any{map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": 0, "delta": reply}, map[string]any{"type": "response.completed", "response": response}} {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	message := map[string]any{"id": id, "type": "message", "role": "assistant", "model": body["model"], "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}
	if body["stream"] != true {
		usage["output_tokens"] = 50
		message["content"] = []any{map[string]any{"type": "text", "text": reply}}
		message["stop_reason"] = "end_turn"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	emit := func(kind string, v map[string]any) {
		v["type"] = kind
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
	}
	emit("message_start", map[string]any{"message": message})
	if m.toolFirst && n == 0 {
		emit("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "synthetic-tool", "name": "FixtureLookup", "input": map[string]any{}}})
		emit("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"key":"synthetic"}`}})
		emit("content_block_stop", map[string]any{"index": 0})
		emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 50}})
		emit("message_stop", map[string]any{})
		return
	}
	emit("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	emit("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": reply}})
	emit("content_block_stop", map[string]any{"index": 0})
	emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 50}})
	emit("message_stop", map[string]any{})
}

type compactionProcess struct {
	root     string
	settings api.RuntimeSettings
	host     *client
	cmd      *exec.Cmd
}

func newCompactionProcess(t *testing.T, ctx context.Context) *compactionProcess {
	return newConfiguredCompactionProcess(t, ctx, nil)
}
func newConfiguredCompactionProcess(t *testing.T, ctx context.Context, config map[string]any) *compactionProcess {
	t.Helper()
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY for actual Core process acceptance")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &compactionProcess{root: root, settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}}
	if config != nil {
		if err := os.MkdirAll(h.settings.CaelisStore, 0700); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(config)
		if err := os.WriteFile(filepath.Join(h.settings.CaelisStore, "config.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	h.start(t, ctx)
	t.Cleanup(func() { h.stop(); h.archive(t) })
	return h
}
func (h *compactionProcess) archive(t *testing.T) {
	out := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR")
	if out == "" {
		return
	}
	out = filepath.Join(out, strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Error(err)
		return
	}
	if b, e := os.ReadFile(filepath.Join(h.root, "host.log")); e == nil {
		_ = os.WriteFile(filepath.Join(out, "host.log"), b, 0600)
	}
	_ = filepath.WalkDir(filepath.Join(h.settings.CaelisStore, "sessions"), func(path string, d os.DirEntry, e error) error {
		if e == nil && !d.IsDir() && strings.HasSuffix(path, ".events.jsonl") {
			b, e := os.ReadFile(path)
			if e == nil {
				_ = os.WriteFile(filepath.Join(out, filepath.Base(path)), b, 0600)
			}
		}
		return nil
	})
}
func (h *compactionProcess) start(t *testing.T, ctx context.Context) {
	t.Helper()
	home := filepath.Join(h.root, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	h.cmd = exec.CommandContext(ctx, h.settings.CLIPath, "serve", "--store-dir", h.settings.CaelisStore, "--listen", "127.0.0.1:0")
	h.cmd.Dir = h.root
	h.cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + os.TempDir()}
	log, err := os.OpenFile(filepath.Join(h.root, "host.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	h.cmd.Stdout, h.cmd.Stderr = log, log
	if err = h.cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	log.Close()
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	waitAcceptance(t, ready, func() bool {
		d, token, err := Discover(h.settings)
		if err != nil {
			return false
		}
		c, err := newClient(d.Endpoint, token)
		if err != nil {
			return false
		}
		if _, err = initialize(ready, c); err != nil {
			c.http.CloseIdleConnections()
			return false
		}
		h.host = c
		return true
	})
}
func (h *compactionProcess) stop() {
	if h.cmd != nil {
		_ = h.cmd.Process.Signal(os.Interrupt)
		_ = h.cmd.Wait()
		h.cmd = nil
	}
	if h.host != nil {
		h.host.http.CloseIdleConnections()
	}
}
func (h *compactionProcess) connectModel(t *testing.T, ctx context.Context, provider, name, base string) string {
	t.Helper()
	var status wire.StatusSnapshot
	if err := h.host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "model-" + digest([]byte(provider + name))[:12]
	var result wire.CommandResult
	req := wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: provider, Model: name, BaseUrl: &base, ApiKey: pointer("SYNTHETIC_ONLY"), ContextWindowTokens: pointer(64000), MaxOutputTokens: pointer(512), ImageInput: pointer(true)}}
	if err := h.host.json(ctx, "POST", "/configuration/connect-model", req, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		t.Fatalf("connect model: %s %s %s %v", result.Outcome, result.ErrorCode, value(result.Detail), err)
	}
	return compactionModelSelector(t, ctx, h.host, name)
}
func compactionModelSelector(t *testing.T, ctx context.Context, host *client, name string) string {
	t.Helper()
	rows, err := rawCatalog(ctx, host, "model")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if value(row.Display) == name || strings.HasSuffix(value(row.Display), "/"+name) || strings.HasSuffix(row.Value, "/"+name) {
			return row.Value
		}
	}
	t.Fatalf("fixture model not offered: %s; catalog=%+v", name, rows)
	return ""
}
func (h *compactionProcess) experiments(t *testing.T, ctx context.Context, async, idle bool) {
	t.Helper()
	dir := filepath.Join(h.root, "preferences")
	v, err := ReadRuntimeExperiments(ctx, h.settings, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, choice := range []struct {
		id string
		on bool
	}{{"async_compact", async}, {"idle_compact", idle}} {
		v, err = ChangeRuntimeExperiments(ctx, h.settings, dir, api.RuntimeExperimentChange{ID: choice.id, Enabled: choice.on, ExpectedRevision: v.Revision})
		if err != nil || v.SaveState != "saved" {
			t.Fatal("experiment save", v.SaveState, err)
		}
	}
}
func (h *compactionProcess) session(t *testing.T, ctx context.Context, selector string) *Session {
	return h.sessionWithTools(t, ctx, selector, noTools{})
}
func (h *compactionProcess) sessionWithTools(t *testing.T, ctx context.Context, selector string, host api.ApplicationTools) *Session {
	t.Helper()
	s := New(Options{Directory: filepath.Join(h.root, "bot"), Settings: h.settings, ToolsOnly: true, RequireApproval: true, Execution: api.ExecutionSettings{Model: selector, Effort: "none", ApprovalMode: "workspace-write"}})
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: host, ApprovedTools: []string{"FixtureLookup"}, Instructions: syntheticHandoff, NotebookDirectory: h.root}); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	return s
}
func submitCompactionText(t *testing.T, ctx context.Context, s *Session, id, text string) {
	t.Helper()
	before := s.Snapshot().CurrentTurn
	r, err := s.Submit(ctx, api.Submission{ID: id, Text: text}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("submit: %s %v", r.Outcome, err)
	}
	waitTurn(t, ctx, s, before)
}
func compactionStatus(t *testing.T, ctx context.Context, s *Session) wire.StatusSnapshot {
	t.Helper()
	var out wire.StatusSnapshot
	if err := s.client.json(ctx, "GET", "/sessions/"+idPath(s.ConversationState().Session)+"/status?diagnostics=true", nil, &out, "", ""); err != nil {
		t.Fatal(err)
	}
	return out
}

// Each case uses the production Bot adapter and a fresh actual Core process.
// HTTP responses are synthetic; the native 60-second timer is not shortened.
func TestCoreCompactionSwitchMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		async, idle, codex, media bool
	}{
		{"off_off", false, false, false, false}, {"async_only", true, false, false, false},
		{"idle_only_media_tools", false, true, false, true}, {"both", true, true, false, false},
		{"magpie_codex", false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
			defer cancel()
			model := &compactionHTTPModel{summaryStarted: make(chan struct{}, 1), toolFirst: tc.media}
			server := httptest.NewServer(http.HandlerFunc(model.serve))
			defer server.Close()
			config := compactionFixtureConfig(server.URL, tc.codex)
			h := newConfiguredCompactionProcess(t, ctx, config)
			h.experiments(t, ctx, tc.async, tc.idle)
			tools := &acceptanceTools{defs: fixtureDefinitions("string")[:1], call: func(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
				if _, ok := api.ToolInvocationFromContext(ctx); !ok {
					t.Error("tool lost native invocation identity")
				}
				return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "SYNTHETIC_TOOL_RESULT"}}}
			}}
			s := h.sessionWithTools(t, ctx, compactionModelSelector(t, ctx, h.host, "native-fixture"), tools)
			seedCompactionTurns(t, ctx, s, h.root, tc.media)
			if !tc.codex {
				s.mu.Lock()
				usage := s.state.Views[s.state.Session.SessionId].Usage
				s.mu.Unlock()
				if usage.CacheReadTokens != 25000 || !usage.CacheReadTokensReported || usage.CacheWriteTokens != 0 || !usage.CacheWriteTokensReported || usage.CacheWrite1hTokens != 0 || !usage.CacheWrite1hTokensReported {
					t.Fatalf("actual Core event lost cache presence: %+v", usage)
				}
			}
			before := s.Snapshot().CurrentTurn
			finished := time.Now()
			if !tc.idle || tc.codex {
				select {
				case <-model.summaryStarted:
					t.Fatal("unexpected idle request")
				case <-time.After(65 * time.Second):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if fixtureCheckpointCount(h.settings.CaelisStore) != 0 {
					t.Fatal("unexpected checkpoint")
				}
			} else {
				select {
				case <-model.summaryStarted:
				case <-ctx.Done():
					t.Fatalf("idle request missing, calls=%d", len(model.observed()))
				}
				if elapsed := time.Since(finished); elapsed < 58*time.Second {
					t.Fatalf("early idle: %s", elapsed)
				} else {
					t.Logf("native idle dwell=%s", elapsed)
				}
				waitAcceptance(t, ctx, func() bool {
					v, e := s.Configuration(ctx)
					return e == nil && v.LastRequest != nil && value(v.LastRequest.JobId) != "" && v.LastRequest.TurnId == ""
				})
				// GET session/status currently acquires Runtime and cancels pending idle.
				// Reading our own synthetic journal avoids that observer effect.
				waitAcceptance(t, ctx, func() bool { return fixtureCheckpointCount(h.settings.CaelisStore) == 1 })
				if s.Snapshot().CurrentTurn != before || !s.Snapshot().CanSend {
					t.Fatal("background job replaced foreground identity")
				}
				checkCompactionPrefix(t, model.observed(), tc.media)
				submitCompactionText(t, ctx, s, "resume", "What remains? Preserve NO_DEPLOY and original receipts.")
				calls := model.observed()
				last, _ := json.Marshal(calls[len(calls)-1].Body)
				if !strings.Contains(string(last), "# Pending") {
					t.Fatal("resume omitted checkpoint")
				}
			}
			calls := model.observed()
			status := compactionStatus(t, ctx, s)
			if got := value(status.Usage.SessionUsageTotal.TotalTokens); got != len(calls)*34050 {
				t.Fatalf("usage not counted once: calls=%d total=%d", len(calls), got)
			}
			writeCompactionEvidence(t, map[string]any{"calls": calls, "status": status.Usage, "async": tc.async, "idle": tc.idle, "foregroundTurn": before})
		})
	}
}
func compactionFixtureConfig(base string, codex bool) map[string]any {
	provider, apiName, model := "anthropic", "anthropic", "claude-fixture"
	if codex {
		provider, apiName, model = "magpie", "openai_responses", "codex/gpt-6-luna"
	}
	return map[string]any{"models": map[string]any{"configs": []any{map[string]any{"alias": "native-fixture", "provider": provider, "api": apiName, "model": model, "base_url": base, "token": "SYNTHETIC_ONLY", "auth_type": "api_key", "context_window_tokens": 64000, "max_output_tokens": 512, "image_input": true}}}}
}
func seedCompactionTurns(t *testing.T, ctx context.Context, s *Session, root string, media bool) {
	t.Helper()
	text := "Goal: synthetic release checklist. NO_DEPLOY. Pending checksum.\n" + strings.Repeat("archived evidence token ", 5000)
	if media {
		path := filepath.Join(root, "synthetic-large.png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2100, 1600))); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		before := s.Snapshot().CurrentTurn
		r, err := s.Submit(ctx, api.Submission{ID: "first", Text: text}, []api.InputFile{{Name: "synthetic-large.png", Path: path}})
		if err != nil || r.Outcome != "accepted" {
			t.Fatal("image submit", r, err)
		}
		waitTurn(t, ctx, s, before)
	} else {
		submitCompactionText(t, ctx, s, "first", text)
	}
	submitCompactionText(t, ctx, s, "second", "Keep the original goal and constraints. Finish only after owner review.\n"+strings.Repeat("more archived evidence ", 1000))
}
func writeCompactionEvidence(t *testing.T, v any) {
	t.Helper()
	out := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR")
	if out == "" {
		return
	}
	out = filepath.Join(out, strings.ReplaceAll(t.Name(), "/", "_"))
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(out, "requests.json"), b, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}
func checkCompactionPrefix(t *testing.T, calls []compactionHTTPCall, media bool) {
	t.Helper()
	var previous, summary map[string]any
	for i, c := range calls {
		if c.Summary {
			summary = c.Body
			previous = calls[i-1].Body
			break
		}
	}
	if summary == nil {
		t.Fatal("missing summary request")
	}
	// Cache breakpoints move to the latest frontier; compare semantic blocks and
	// separately validate every native provider cache marker on the actual bytes.
	strip := func(v any) any { return withoutCacheMarkers(v) }
	for _, field := range []string{"system", "tools"} {
		x, _ := json.Marshal(strip(previous[field]))
		y, _ := json.Marshal(strip(summary[field]))
		if !bytes.Equal(x, y) {
			t.Fatalf("summary rewrote %s prefix", field)
		}
	}
	a := previous["messages"].([]any)
	b := summary["messages"].([]any)
	if len(b) < len(a) {
		t.Fatal("summary dropped request prefix")
	}
	for i := range a {
		x, _ := json.Marshal(strip(a[i]))
		y, _ := json.Marshal(strip(b[i]))
		if !bytes.Equal(x, y) {
			t.Fatalf("summary rewrote prefix message %d", i)
		}
	}
	markers, images, pairs := 0, 0, map[string]int{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["cache_control"].(map[string]any); ok {
				markers++
				if c["type"] != "ephemeral" || c["ttl"] != "5m" {
					t.Error("unexpected native cache marker", c)
				}
			}
			if x["type"] == "image" {
				source, _ := x["source"].(map[string]any)
				data, _ := source["data"].(string)
				raw, e := base64.StdEncoding.DecodeString(data)
				cfg, _, de := image.DecodeConfig(bytes.NewReader(raw))
				if e != nil || de != nil || cfg.Width > 2048 || cfg.Height > 2048 {
					t.Error("unprepared image prefix", cfg, e, de)
				}
				images++
			}
			if x["type"] == "tool_use" {
				id, _ := x["id"].(string)
				pairs[id]++
			}
			if x["type"] == "tool_result" {
				id, _ := x["tool_use_id"].(string)
				pairs[id]--
			}
			for _, value := range x {
				walk(value)
			}
		case []any:
			for _, value := range x {
				walk(value)
			}
		}
	}
	walk(summary)
	if markers == 0 || markers > 4 {
		t.Fatal("native cache marker count", markers)
	}
	if media && (images == 0 || len(pairs) == 0) {
		t.Fatal("image/tool pair missing")
	}
	for id, n := range pairs {
		if n != 0 {
			t.Fatalf("unpaired tool %s: %d", id, n)
		}
	}
	t.Logf("actual request prefix messages=%d cache_markers=%d prepared_images=%d paired_tools=%d", len(a), markers, images, len(pairs))
}
func withoutCacheMarkers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			if k != "cache_control" {
				out[k] = withoutCacheMarkers(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = withoutCacheMarkers(value)
		}
		return out
	default:
		return v
	}
}

func fixtureCheckpointCount(store string) int {
	count := 0
	_ = filepath.WalkDir(filepath.Join(store, "sessions"), func(path string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.HasSuffix(path, ".events.jsonl") {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		for _, line := range bytes.Split(raw, []byte("\n")) {
			var event map[string]any
			if json.Unmarshal(line, &event) == nil && event["type"] == "compact" {
				count++
			}
		}
		return nil
	})
	return count
}

func TestCoreExperimentCatalogCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	h := newCompactionProcess(t, ctx)
	directory := filepath.Join(h.root, "preferences")
	before, err := ReadRuntimeExperiments(ctx, h.settings, directory)
	if err != nil || !before.Supported {
		t.Fatal("catalog unavailable", before, err)
	}
	asyncOffered := false
	for _, feature := range before.Features {
		if feature.ID == "async_compact" {
			asyncOffered = true
		}
		if feature.ID != "async_compact" && feature.ID != "idle_compact" {
			t.Fatal("retired control exposed", feature.ID)
		}
	}
	if !asyncOffered {
		t.Fatal("async capability missing")
	}
	saved, err := ChangeRuntimeExperiments(ctx, h.settings, directory, api.RuntimeExperimentChange{ID: "async_compact", Enabled: true, ExpectedRevision: before.Revision})
	if err != nil || saved.SaveState != "saved" {
		t.Fatal("dynamic catalog save failed", saved, err)
	}
	writeCompactionEvidence(t, map[string]any{"before": before, "saved": saved})
	t.Logf("actual runtime catalog size=%d; async saved with original receipt", len(before.Features))
}
