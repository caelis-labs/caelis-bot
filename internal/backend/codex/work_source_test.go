package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkSourceRequiresNativeActivationAndPreservesOpaqueBinding(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	if _, err := s.WorkDispatchSource(t.Context()); err == nil {
		t.Fatal("idle conversation attested authority")
	}
	sendSynthetic(t, s, "source-parent-request")
	source, err := s.WorkDispatchSource(t.Context())
	if err != nil || source.NodeID != api.LocalNodeID || source.Backend != "codex" || source.BindingID != "thread-native" || source.OperationID != "run-native" || source.Kind != "native_activation" {
		t.Fatal(source, err)
	}
	b, err := json.Marshal(api.TaskMessage{Source: source, ID: "task-id", RequestID: "request-id", Prompt: "Assignment"})
	if err != nil || strings.Contains(string(b), "run-native") || strings.Contains(string(b), "thread-native") {
		t.Fatal("authority leaked into model DTO", string(b), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.WorkDispatchSource(ctx); err == nil {
		t.Fatal("cancelled invocation attested authority")
	}
	s.mu.Lock()
	s.run = ""
	s.mu.Unlock()
	if _, err := s.WorkDispatchSource(t.Context()); err == nil {
		t.Fatal("stale prompt became authority")
	}
}
