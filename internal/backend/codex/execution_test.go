package codex

import (
	"errors"
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
	v.ServiceTier = ""
	if err = s.ChangeExecution(testContext(t), v, persist); err == nil {
		t.Fatal("changed policy during active work")
	}
	after := s.Snapshot()
	if after.Revision != before.Revision || after.LastReceipt != before.LastReceipt {
		t.Fatal("rejected preferences changed live execution")
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
