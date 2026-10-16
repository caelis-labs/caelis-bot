package statemigration

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUpgradeKeepsOriginalEffectIdentityAndSeparatesResourceReceipt(t *testing.T) {
	raw := []byte(`{"version":1,"principalID":"owner","connection":{"principal_id":"owner","application_id":"app","connection_id":"connection"},"session":{"session_id":"sid"},"operations":{"same":{"path":"/application/sessions/sid/prompt","body":{"operation_id":"same","input":"unchanged"},"digest":"original","outcome":"unknown","source":{"operation_id":"same","kind":"user"}}},"typed":{"same":{"path":"/application/sessions/sid/resources","body":{"data":"cHJpdmF0ZQ=="},"outcome":"unknown"}},"calls":{"call":{"call":{"id":"call","source":{"operation_id":"same"}},"phase":"executing"}}}`)
	out, migrated, err := Upgrade(raw)
	if err != nil || !migrated {
		t.Fatalf("upgrade = %v, %v", migrated, err)
	}
	var state struct {
		Version    int
		Connection json.RawMessage
		Operations map[string]struct {
			ReceiptID, Path, Digest, Outcome string
			Body                             json.RawMessage
			Source                           struct {
				OperationID string `json:"operation_id"`
			}
		}
		Typed map[string]struct{ ReceiptID, Outcome string }
		Calls json.RawMessage
	}
	if err := json.Unmarshal(out, &state); err != nil {
		t.Fatal(err)
	}
	command, resource := state.Operations["same"], state.Typed["same"]
	if state.Version != 2 || state.Connection != nil || command.Path != "/sessions/sid/prompt" || command.Digest != "original" || command.Outcome != "unknown" || command.Source.OperationID != "same" || resource.Outcome != "unknown" || !bytes.Contains(command.Body, []byte("unchanged")) || !bytes.Contains(state.Calls, []byte("executing")) {
		t.Fatalf("identity or uncertainty lost: %s", out)
	}
	// Shared Core applicationv3 contract: SHA256(JSON array), not concatenation.
	if command.ReceiptID != "retired-app-op-1f4af84426d67661384c072f4e068488c1a1bebbb2ec1869b716da0e71c92d3e" || resource.ReceiptID != "retired-app-resource-op-f0e8e80c23d16f753f92b54e7e4ef2aca17573a0d462edc764b5737701de3d61" {
		t.Fatalf("receipt mapping changed: %s / %s", command.ReceiptID, resource.ReceiptID)
	}
	again, migrated, err := Upgrade(out)
	if err != nil || migrated || !bytes.Equal(again, out) {
		t.Fatalf("second import changed current state: %v, %v", migrated, err)
	}
}

func TestUpgradeRefusesAmbiguousOwnerAndFutureState(t *testing.T) {
	for _, raw := range []string{
		`{"version":3}`,
		`{"version":1,"principalID":"one","connection":{"principal_id":"two"}}`,
		`{"version":1,"operations":{"id":{"path":"/sessions/x/prompt"}}}`,
		`{"version":1,"principalID":"one","connection":{"application_id":"app","connection_id":"conn"},"typed":{"id":{"path":"/application/sessions//resources"}}}`,
	} {
		if _, _, err := Upgrade([]byte(raw)); err == nil {
			t.Fatalf("accepted ambiguous state: %s", raw)
		}
	}
}
