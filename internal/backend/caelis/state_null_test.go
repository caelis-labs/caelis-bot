package caelis

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestNullJournalBodyPreservesOriginalReceiptWithoutDispatch(t *testing.T) {
	for _, outcome := range []string{"accepted", "committed", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "runtime")
			s := New(Options{Directory: directory})
			s.state.Operations["original"] = journal{HostNotice: true, Body: json.RawMessage("null"), Digest: "original-digest", Outcome: outcome, Path: "/sessions/original/prompt"}
			if err := s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			restored := New(Options{Directory: directory})
			// No native client exists: an original readback must never try a POST.
			receipt, err := restored.SubmitSystem(t.Context(), api.SystemMessage{ID: "original", Source: api.SystemWorkerResult, Text: "Original record"})
			if err != nil || receipt.ID != "original" || receipt.Outcome != productOutcome(wire.Outcome(outcome)) {
				t.Fatal(receipt, err)
			}
			record := restored.state.Operations["original"]
			if record.Digest != "original-digest" || record.Outcome != outcome || len(record.Body) != 0 {
				t.Fatal("original receipt changed", record.Outcome)
			}
		})
	}
}
