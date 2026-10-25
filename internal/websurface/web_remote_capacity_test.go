package websurface

import (
	"context"
	"fmt"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type isolationReviewLocalSubmit struct {
	testEngine
	calls int
}

func (e *isolationReviewLocalSubmit) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.calls++
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}

func TestReviewHistoricalRemotePairReceiptsCannotExhaustLocalWebAdmission(t *testing.T) {
	e := &isolationReviewLocalSubmit{}
	s := testServer(t, e)
	c := pairClient(t, s)
	c.claim(t, false)
	for n := range 4096 {
		s.operations[fmt.Sprintf("old-remote-pair-%04d", n)] = operation{Owner: sessionIDFor(c), Digest: "retained-original-digest", State: "complete", Error: "operation_unconfirmed", BotConnection: &api.BotConnectionReference{ID: "remote-B", Pairing: fmt.Sprint(n), Proof: "retained-original-proof"}}
	}
	w := c.request("/api/v1/conversation/submit", map[string]any{"id": "independent-local-message", "args": []any{api.Submission{ID: "local-native-message", Text: "controlled-local-input"}}})
	if w.Code != 200 || e.calls != 1 {
		t.Fatalf("retained remote-only receipts exhaust local Web actions: status=%d body=%s localEffects=%d", w.Code, w.Body.String(), e.calls)
	}
}
