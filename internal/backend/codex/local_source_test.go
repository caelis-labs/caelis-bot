package codex

import (
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func TestRoutedLocalTaskPersistsNativeSourceInOriginalStartAndMessageReceipts(t *testing.T) {
	s, f, d, _ := taskPair(t)
	m, err := tasks.OpenRouted(filepath.Join(filepath.Dir(s.opts.StateFile), "product-tasks.json"), s.workRoot(), "codex", s, s, s.Snapshot, nil, s)
	if err != nil {
		t.Fatal(err)
	}
	sendSynthetic(t, s, "attested-parent-request")
	source, err := s.WorkDispatchSource(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	in := api.TaskStart{RequestID: "attested-local-start", Title: "Synthetic", Prompt: "Synthetic work"}
	f.mu.Lock()
	d.uncertain = true
	f.mu.Unlock()
	v, err := m.StartTask(testContext(t), in)
	if err == nil || v.Outcome != "unknown" {
		t.Fatal("missing original unknown receipt", v, err)
	}
	assertLocalReceiptSource(t, s, v.ID, in.RequestID, source)
	if _, err := m.StartTask(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadTask(testContext(t), v.ID); err != nil {
		t.Fatal(err)
	}
	msg := api.TaskMessage{ID: v.ID, RequestID: "attested-local-message", Prompt: "Continue", Source: api.WorkDispatchSource{Kind: "fabricated"}, RequestDigest: "fabricated"}
	if result, err := m.SendTask(testContext(t), msg); err == nil || result.Outcome != "unknown" {
		t.Fatal("missing original unknown continuation", result, err)
	}
	assertLocalReceiptSource(t, s, v.ID, msg.RequestID, source)
	if _, err := m.SendTask(testContext(t), msg); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if starts != 1 || sends != 2 {
		t.Fatal("unknown original request replayed", starts, sends)
	}
}

func assertLocalReceiptSource(t *testing.T, s *Session, task, request string, source api.WorkDispatchSource) {
	t.Helper()
	// Inspect the persisted native binding, not an in-memory task projection.
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if loaded.loadErr != nil {
		t.Fatal(loaded.loadErr)
	}
	r := loaded.binding.Tasks[task].Requests[request]
	if r.Source == nil || *r.Source != source || len(r.RequestDigest) != 64 || r.Outcome != "unknown" {
		t.Fatal("native original receipt lost attestation", r)
	}
}

func TestRoutedLocalLegacyReceiptsAreNotBackfilledOnReconciliation(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "legacy-parent-request")
	v := newTask(t, m, "legacy-local-start")
	f.mu.Lock()
	d.uncertain = true
	f.mu.Unlock()
	msg := api.TaskMessage{ID: v.ID, RequestID: "legacy-local-message", Prompt: "Continue"}
	if _, err := m.SendTask(testContext(t), msg); err == nil {
		t.Fatal("missing legacy unknown receipt")
	}
	s.mu.Lock()
	originalStart := s.binding.Tasks[v.ID].Requests["legacy-local-start"]
	originalMessage := s.binding.Tasks[v.ID].Requests[msg.RequestID]
	s.mu.Unlock()
	routed, err := tasks.OpenRouted(filepath.Join(filepath.Dir(s.opts.StateFile), "product-tasks.json"), s.workRoot(), "codex", s, s, s.Snapshot, nil, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routed.StartTask(testContext(t), api.TaskStart{RequestID: "legacy-local-start", Title: "Synthetic task", Prompt: "Make a synthetic artifact in your workspace; no external writes."}); err != nil {
		t.Fatal(err)
	}
	msg.Source = api.WorkDispatchSource{Kind: "fabricated"}
	if _, err := routed.SendTask(testContext(t), msg); err != nil {
		t.Fatal(err)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if loaded.loadErr != nil {
		t.Fatal(loaded.loadErr)
	}
	for request, original := range map[string]taskReceipt{"legacy-local-start": originalStart, msg.RequestID: originalMessage} {
		r := loaded.binding.Tasks[v.ID].Requests[request]
		if r.Source != nil || r != original {
			t.Fatal("legacy receipt acquired fabricated provenance", r)
		}
	}
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if starts != 1 || sends != 2 {
		t.Fatal("legacy reconciliation dispatched again", starts, sends)
	}
}
