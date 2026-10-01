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

func TestManagedWorkSourceAnnotationPreservesNativeActivation(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	sendSynthetic(t, s, "managed-source-parent")
	s.ConfigureDispatchSource(func(ctx context.Context, source api.WorkDispatchSource) (api.WorkDispatchSource, error) {
		if source.BindingID != "thread-native" || source.OperationID != "run-native" {
			t.Fatal("wrapper lost native activation", source)
		}
		source.NodeID = "managed-node"
		source.Lease = api.WorkerLeaseGrant{BotID: "stable-bot", BrokerNodeID: "paired-broker", SourceNodeID: "managed-node", Backend: "codex", Epoch: "opaque-epoch"}
		return source, source.Validate()
	})
	source, err := s.WorkDispatchSource(t.Context())
	if err != nil || source.NodeID != "managed-node" || source.Lease.SourceNodeID != "managed-node" {
		t.Fatal(source, err)
	}
}
