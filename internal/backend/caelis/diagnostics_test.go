package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func TestNonblockingRuntimeErrorsAreLoggedWithoutChatNotices(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", NextCursor: pointer("next"), Events: []wire.Envelope{
			{Kind: "caelis/error", Scope: pointer("tool"), SessionId: pointer("main"), TurnId: pointer("turn"), EventId: pointer("error-id"), Error: pointer("No such file or directory PRIVATE_SENTINEL")},
			{Kind: "caelis/lifecycle", Lifecycle: &wire.LifecycleEvent{State: "completed"}},
		}})
	})
	dir := t.TempDir()
	s.diagnostics = diagnosticlog.New(dir)
	_ = s.watch(t.Context(), s.client, "main", "instance")
	v := s.Snapshot()
	if v.Message != "" || v.Phase != "completed" || !v.CanSend {
		t.Fatalf("component error became a user failure: %+v", v)
	}
	b, err := os.ReadFile(filepath.Join(dir, "error.jsonl"))
	if err != nil || strings.Contains(string(b), "PRIVATE_SENTINEL") || !strings.Contains(string(b), "error-id") || !strings.Contains(string(b), "executable or file not found") {
		t.Fatal("missing or unsafe runtime diagnostics")
	}
}
