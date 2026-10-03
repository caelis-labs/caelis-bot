package codex

import (
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func catalogEntry() map[string]any {
	return map[string]any{"model": "test-model", "displayName": "Test model", "isDefault": true, "defaultReasoningEffort": "high", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "high"}}, "serviceTiers": []any{map[string]any{"id": "fast", "name": "Fast"}}}
}
func TestExecutionSettingsReachNewTurnsAndRejectInvalidChanges(t *testing.T) {
	s, f := sessionPair(t, "")
	f.mu.Lock()
	f.modelPages = map[string]any{"": map[string]any{"data": []any{catalogEntry()}, "nextCursor": "more"}, "more": map[string]any{"data": []any{catalogEntry()}}}
	f.mu.Unlock()
	catalog, err := s.Models(testContext(t))
	if err != nil || len(catalog) != 1 || len(catalog[0].Efforts) != 1 {
		t.Fatalf("catalog: %v %v", catalog, err)
	}
	v := api.ExecutionSettings{Model: "test-model", Effort: "high", ServiceTier: "fast", ApprovalMode: "ask"}
	saved := 0
	persist := func() error { saved++; return nil }
	if err = s.ChangeExecution(testContext(t), v, persist); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Submit(testContext(t), api.Submission{ID: "preferences-fast", Text: "synthetic"}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("submit %v %v", receipt, err)
	}
	f.mu.Lock()
	params := f.lastParams
	f.mu.Unlock()
	for key, want := range map[string]string{"model": `"test-model"`, "effort": `"high"`, "serviceTier": `"fast"`, "approvalPolicy": `"on-request"`, "approvalsReviewer": `"user"`} {
		if string(params[key]) != want {
			t.Fatalf("%s=%s", key, params[key])
		}
	}
	before := s.Snapshot()
	executionBefore := v
	v.ServiceTier = ""
	if err = s.ChangeExecution(testContext(t), v, persist); err == nil {
		t.Fatal("changed policy during active work")
	}
	// Native progress can advance the view while a preference edit is rejected.
	// Fence on the real event projection instead of relying on goroutine timing.
	f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "itemId": "preference-progress", "delta": "synthetic progress"})})
	after := awaitState(t, s, func(view api.Snapshot) bool {
		for _, item := range view.Items {
			if item.Text == "synthetic progress" {
				return true
			}
		}
		return false
	})
	s.mu.Lock()
	executionAfter := s.opts.Execution
	s.mu.Unlock()
	if executionAfter != executionBefore || saved != 1 {
		t.Fatalf("rejected preferences changed execution or persisted: settings %+v, saves %d", executionAfter, saved)
	}
	if after.LastReceipt != before.LastReceipt || after.CurrentTurn != before.CurrentTurn || !after.CanSteer {
		t.Fatal("rejected preferences changed the accepted turn")
	}
	receipt, err = s.Submit(testContext(t), api.Submission{ID: "preferences-steer", Text: "synthetic"}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("steer: %v %v", receipt, err)
	}
	f.mu.Lock()
	steer := f.lastParams
	f.mu.Unlock()
	for _, key := range []string{"model", "effort", "serviceTier", "approvalPolicy", "approvalsReviewer", "sandboxPolicy"} {
		if _, ok := steer[key]; ok {
			t.Fatalf("steer rewrote %s", key)
		}
	}
	if err = s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if err = s.ChangeExecution(testContext(t), v, persist); err != nil {
		t.Fatal(err)
	}
	receipt, err = s.Submit(testContext(t), api.Submission{ID: "preferences-standard", Text: "synthetic"}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("submit %v %v", receipt, err)
	}
	f.mu.Lock()
	tier := string(f.lastParams["serviceTier"])
	f.mu.Unlock()
	if tier != "null" {
		t.Fatalf("Fast was not explicitly cleared: %s", tier)
	}
	if err = s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	for _, bad := range []api.ExecutionSettings{{Model: "missing", Effort: "high"}, {Model: "test-model", Effort: "made-up"}, {Model: "test-model", Effort: "high", ServiceTier: "made-up"}, {Model: "test-model", Effort: "high", ApprovalMode: "made-up"}} {
		if err = s.ChangeExecution(testContext(t), bad, persist); err == nil {
			t.Fatalf("accepted unsupported settings %+v", bad)
		}
	}
	if saved != 2 {
		t.Fatalf("invalid preference persisted: %d", saved)
	}
	changed := v
	changed.ApprovalMode = "full-access"
	if err = s.ChangeExecution(testContext(t), changed, func() error { return errors.New("disk failure") }); err == nil {
		t.Fatal("expected disk failure")
	}
	if s.opts.Execution != v {
		t.Fatal("failed save changed execution")
	}
}
func TestExecutionModesPreserveAcceptanceSandbox(t *testing.T) {
	for _, mode := range []string{"auto", "ask", "read-only", "full-access"} {
		s := NewSession(SessionOptions{Directory: t.TempDir(), Execution: api.ExecutionSettings{Model: "m", Effort: "high", ApprovalMode: mode}})
		p := s.connectionParams()
		q := map[string]any{}
		s.applyExecution(q, false)
		if p["model"] != "m" || q["effort"] != "high" {
			t.Fatal("thread/turn configuration diverged")
		}
		kinds := map[string]string{"auto": "workspaceWrite", "ask": "workspaceWrite", "read-only": "readOnly", "full-access": "dangerFullAccess"}
		if q["sandboxPolicy"].(map[string]any)["type"] != kinds[mode] {
			t.Fatal(mode, q)
		}
		s.opts.RequireApproval = true
		p = s.connectionParams()
		q = map[string]any{}
		s.applyExecution(q, false)
		if p["sandbox"] != "workspace-write" || q["approvalPolicy"] != "untrusted" || q["approvalsReviewer"] != "user" {
			t.Fatal("acceptance flag weakened", mode, p, q)
		}
	}
}
func TestModelPaginationRejectsCycle(t *testing.T) {
	s, f := sessionPair(t, "")
	f.mu.Lock()
	f.modelPages = map[string]any{"": map[string]any{"nextCursor": "loop"}, "loop": map[string]any{"nextCursor": "loop"}}
	f.mu.Unlock()
	if _, err := s.Models(testContext(t)); err == nil {
		t.Fatal("cyclic catalog accepted")
	}
}
func TestComposerSnapshotIsIsolatedAndExcludesTranscript(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: t.TempDir() + "/binding"})
	s.state.Items = []api.Item{{Text: strings.Repeat("synthetic", 100000)}}
	s.state.References = []api.Reference{{ID: "one", Name: "reference"}}
	s.state.Approvals = []api.Approval{{Status: "pending", Action: "private command"}, {Status: "resolved"}}
	s.state.CanSend = false
	v := s.ComposerSnapshot()
	if len(v.Items) != 0 || len(v.Approvals) != 1 || v.Approvals[0].Action != "" || v.CanSend {
		t.Fatal("quick projection leaked transcript/authority")
	}
	v.References[0].Name = "changed"
	v.Approvals[0].Status = "resolved"
	if s.state.References[0].Name != "reference" || s.state.Approvals[0].Status != "pending" {
		t.Fatal("projection aliases live state")
	}
}
func BenchmarkComposerSnapshot(b *testing.B) {
	for _, full := range []bool{false, true} {
		name := "quick"
		if full {
			name = "full"
		}
		b.Run(name, func(b *testing.B) {
			s := NewSession(SessionOptions{})
			for range 2000 {
				s.state.Items = append(s.state.Items, api.Item{Text: strings.Repeat("x", 1024)})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if full {
					s.Snapshot()
				} else {
					s.ComposerSnapshot()
				}
			}
		})
	}
}

func TestScreenModelCapabilitySwitchAndDispatchRecheck(t *testing.T) {
	s, f := sessionPair(t, "")
	vision := catalogEntry()
	vision["model"] = "vision"
	vision["inputModalities"] = []string{"text", "image"}
	text := catalogEntry()
	text["model"] = "text"
	text["isDefault"] = false
	text["inputModalities"] = []string{"text"}
	absent := catalogEntry()
	absent["model"] = "unknown"
	absent["isDefault"] = false
	f.mu.Lock()
	f.modelPages = map[string]any{"": map[string]any{"data": []any{vision, text, absent}}}
	f.mu.Unlock()
	for _, choice := range []struct{ model, state string }{{"text", "unsupported"}, {"unknown", "unknown"}, {"vision", "supported"}} {
		err := s.ChangeExecution(t.Context(), api.ExecutionSettings{Model: choice.model, Effort: "high"}, func() error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ImageInput(t.Context())
		if err != nil || got.State != choice.state || got.Model != choice.model {
			t.Fatal(got, err)
		}
		if choice.state != "supported" {
			receipt, err := s.Submit(t.Context(), api.Submission{ID: "screen-" + choice.model, Text: "synthetic", ScreenInput: true}, nil)
			if err != nil || receipt.Outcome != "rejected" {
				t.Fatal("dispatch ignored gate", receipt, err)
			}
			// Reference refresh may independently advance the view revision.
			// Assert the actual dispatch and pending input instead of UI timing.
			f.mu.Lock()
			starts := f.starts
			f.mu.Unlock()
			s.mu.Lock()
			pending := s.binding.Pending != nil
			s.mu.Unlock()
			if starts != 0 || pending {
				t.Fatal("rejected screen input was dispatched or staged", starts, pending)
			}
		}
	}
	// UI's earlier supported result is not authority after the model changes.
	if err := s.ChangeExecution(t.Context(), api.ExecutionSettings{Model: "text", Effort: "high"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Submit(t.Context(), api.Submission{ID: "screen-stale-button", Text: "synthetic", ScreenInput: true}, nil)
	if err != nil || receipt.Outcome != "rejected" {
		t.Fatal(receipt, err)
	}
	f.mu.Lock()
	starts := f.starts
	f.mu.Unlock()
	if starts != 0 {
		t.Fatal("stale enabled button dispatched screen input", starts)
	}
}

func TestScreenInputDeliversBothImagesWithCurrentModel(t *testing.T) {
	s, f := sessionPair(t, "")
	m := catalogEntry()
	m["inputModalities"] = []string{"text", "image"}
	f.mu.Lock()
	f.modelPages = map[string]any{"": map[string]any{"data": []any{m}}}
	f.mu.Unlock()
	if err := s.ChangeExecution(t.Context(), api.ExecutionSettings{Model: "test-model", Effort: "high"}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := []api.InputFile{}
	for _, name := range []string{"selection.png", "context.jpg"} {
		path := filepath.Join(dir, name)
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		pixels := image.NewRGBA(image.Rect(0, 0, 10, 10))
		if name == "selection.png" {
			err = png.Encode(out, pixels)
		} else {
			err = jpeg.Encode(out, pixels, nil)
		}
		out.Close()
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, api.InputFile{Name: name, Path: path})
	}
	receipt, err := s.Submit(t.Context(), api.Submission{ID: "screen-image-pair", Text: "synthetic screen input", ScreenInput: true}, files)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	f.mu.Lock()
	raw := append([]byte{}, f.lastParams["input"]...)
	f.mu.Unlock()
	var input []map[string]any
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if len(input) != 3 || input[0]["type"] != "text" || input[1]["type"] != "localImage" || input[2]["type"] != "localImage" {
		t.Fatalf("lost native image pair: %s", raw)
	}
	// Confirm the Runtime owns copies before the capture owner removes temporary files.
	os.RemoveAll(dir)
	for _, item := range input[1:] {
		if _, err = os.Stat(item["path"].(string)); err != nil {
			t.Fatal("model image depended on cleared capture", err)
		}
	}
}
