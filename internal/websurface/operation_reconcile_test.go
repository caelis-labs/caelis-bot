package websurface

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type receiptEngine struct {
	api.Engine
	mu    sync.Mutex
	view  api.Snapshot
	calls int
}

func (e *receiptEngine) Snapshot() api.Snapshot { e.mu.Lock(); defer e.mu.Unlock(); return e.view }
func (e *receiptEngine) set(owner, id, outcome string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.view = api.Snapshot{RuntimeOwner: owner, Connection: "ready", CanSend: true, LastReceipt: api.Receipt{ID: id, Outcome: outcome}}
}
func (e *receiptEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.view.LastReceipt = api.Receipt{ID: in.ID, Outcome: "unknown"}
	return e.view.LastReceipt, nil
}
func TestOriginalNativeReceiptSettlesIngressWithoutReplay(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			engine := &receiptEngine{}
			engine.set("original-owner", "", "")
			s := testServer(t, engine)
			c := pairClient(t, s)
			c.claim(t, false)
			request := map[string]any{"id": "web-original-operation", "args": []any{api.Submission{ID: "native-original", Text: "fixture"}}}
			if got := c.request("/api/v1/conversation/submit", request); got.Code != 200 {
				t.Fatal(got.Code, got.Body.String())
			}
			if s.UpdateQuiescent() {
				t.Fatal("unknown released")
			}
			// Neither ready/idle, another ID, nor another owner may settle it.
			for _, wrong := range [][2]string{{"original-owner", "different-id"}, {"new-owner", "native-original"}} {
				engine.set(wrong[0], wrong[1], outcome)
				if s.UpdateQuiescent() {
					t.Fatal("unrelated evidence settled original")
				}
			}
			engine.set("original-owner", "native-original", outcome)
			if outcome == "rejected" && !s.UpdateQuiescent() {
				t.Fatal("update admission did not reconcile original rejection")
			}
			r := httptest.NewRequest("GET", testOrigin+"/api/v1/operations/web-original-operation", nil)
			r.AddCookie(c.cookie)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response struct {
				Value api.Receipt `json:"value"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Value.Outcome != outcome {
				t.Fatal(w.Body.String(), err)
			}
			// Re-read the durable ingress after owner projection changes.
			engine.set("original-owner", "later-receipt", "accepted")
			s.operations = nil
			if err := s.loadOperations(); err != nil {
				t.Fatal(err)
			}
			if !s.UpdateQuiescent() {
				t.Fatal("settled receipt still blocks update")
			}
			if got := c.request("/api/v1/conversation/submit", request); got.Code != 200 || engine.calls != 1 {
				t.Fatal("original replayed", got.Body.String())
			}
			request["id"] = "explicit-next-operation"
			request["args"] = []any{api.Submission{ID: "native-next", Text: "next fixture"}}
			if got := c.request("/api/v1/conversation/submit", request); got.Code != 200 || engine.calls != 2 {
				t.Fatal("next explicit action blocked", got.Body.String())
			}
		})
	}
}

func TestNativeSettlementMustBeDurableAndCannotGuessLegacyOwner(t *testing.T) {
	engine := &receiptEngine{}
	engine.set("original-owner", "native-original", "accepted")
	s := testServer(t, engine)
	original := operation{State: "complete", Value: json.RawMessage(`{"id":"native-original","outcome":"unknown"}`)}
	s.operations["legacy"] = original
	if s.UpdateQuiescent() {
		t.Fatal("guessed missing legacy owner")
	}
	delete(s.operations, "legacy")
	original.Native = &nativeReceipt{Owner: "original-owner", ID: "native-original"}
	s.operations["original"] = original
	path := filepath.Join(s.config.Directory, "operations.json")
	// Fail the actual durable replacement, not a mocked successful writer.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if s.UpdateQuiescent() || operationTerminal(s.operations["original"]) {
		t.Fatal("unpersisted settlement released admission")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !s.UpdateQuiescent() {
		t.Fatal("confirmed original could not settle after storage recovered")
	}
}
