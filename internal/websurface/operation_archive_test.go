package websurface

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRejectedIngressIsTerminalAndCorrectionHasNewIdentity(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	calls := 0
	s.register(action{Path: "/api/test/refusal", Function: func(value string) (string, error) {
		calls++
		if value == "invalid" {
			return "", &api.ActionRejectedError{Code: "credential"}
		}
		return "connected", nil
	}})
	first := map[string]any{"id": "original-invalid-token", "args": []string{"invalid"}}
	if got := c.request("/api/test/refusal", first); got.Code != 409 || !strings.Contains(got.Body.String(), "operation_rejected") {
		t.Fatal(got.Body.String())
	}
	if !s.UpdateQuiescent() {
		t.Fatal("known rejection blocks update")
	}
	if got := c.request("/api/test/refusal", first); got.Code != 409 || calls != 1 {
		t.Fatal("rejection replayed")
	}
	if got := c.request("/api/test/refusal", map[string]any{"id": "explicit-correction-id", "args": []string{"valid"}}); got.Code != 200 || calls != 2 {
		t.Fatal(got.Body.String())
	}
	if !s.UpdateQuiescent() {
		t.Fatal("successful correction blocked")
	}
}
func TestTerminalRetirementSurvivesCapacityRestartAndNeverReplays(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	// Faithfully migrate an old oversized journal; unknown records never retire.
	unknown := operation{Owner: sessionIDFor(c), State: "pending", Digest: "original"}
	s.operations["unknown-original"] = unknown
	for n := 0; n < 4200; n++ {
		s.operations[fmt.Sprintf("old-%016d", n)] = operation{Owner: sessionIDFor(c), State: "complete", Digest: "effect", Value: json.RawMessage(`"` + strings.Repeat("a", 4096) + `"`)}
	}
	bytes, _ := json.Marshal(s.operations)
	if len(bytes) <= 16<<20 {
		t.Fatal("fixture too small")
	}
	if err := os.WriteFile(filepath.Join(s.config.Directory, "operations.json"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	s.operations = nil
	if err := s.loadOperations(); err != nil {
		t.Fatal(err)
	}
	if len(s.operations) > 129 {
		t.Fatal("terminal payloads not retired", len(s.operations))
	}
	if s.operations["unknown-original"].Digest != "original" || s.UpdateQuiescent() {
		t.Fatal("unknown lost")
	}
	if err := s.retired.Close(); err != nil {
		t.Fatal(err)
	}
	s.retired = nil
	op, ok, err := s.lookupOperation("old-0000000000000000")
	if err != nil || !ok || op.Error != "operation_retired" {
		t.Fatal(op, ok, err)
	}
	calls := 0
	s.register(action{Path: "/api/test/new", Function: func() string { calls++; return "new" }})
	if got := c.request("/api/test/new", map[string]any{"id": "old-0000000000000000", "args": []any{}}); got.Code != 409 || calls != 0 {
		t.Fatal("retired identity replayed")
	}
	if got := c.request("/api/test/new", map[string]any{"id": "new-explicit-operation", "args": []any{}}); got.Code != 200 || calls != 1 {
		t.Fatal("capacity still blocked", got.Body.String())
	}
}

func TestLargeUnknownReceiptKeepsOriginalReconciliationTarget(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	value := map[string]string{"outcome": "unknown", "id": "native-original", "detail": strings.Repeat("fixture", 6000)}
	s.register(action{Path: "/api/test/unknown", Function: func() any { return value }})
	if got := c.request("/api/test/unknown", map[string]any{"id": "original-large-unknown", "args": []any{}}); got.Code != 200 {
		t.Fatal(got.Code)
	}
	var stored map[string]string
	if err := json.Unmarshal(s.operations["original-large-unknown"].Value, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["id"] != "native-original" || stored["detail"] != value["detail"] || s.UpdateQuiescent() {
		t.Fatal("unknown reconciliation evidence trimmed")
	}
}
