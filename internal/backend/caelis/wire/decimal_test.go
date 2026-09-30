package wire

import (
	"encoding/json"
	"testing"
)

func TestNativeWorkerReceiptNumericRevision(t *testing.T) {
	raw := []byte(`{"operation_id":"work-prompt-fixture","outcome":"committed","result":{"operation_id":"work-prompt-fixture","outcome":"committed","session_id":"worker-fixture","revision":2,"target":{"handle_id":"handle-1","run_id":"run-2","turn_id":"turn-fixture"}}}`)
	var receipt ApplicationOperation
	if err := json.Unmarshal(raw, &receipt); err != nil || receipt.Result == nil || receipt.Result.Revision == nil || *receipt.Result.Revision != "2" {
		t.Fatal("native receipt incompatible", err)
	}
	var outer struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &outer) != nil {
		t.Fatal("receipt fixture envelope")
	}
	var command CommandResult
	if err := json.Unmarshal(outer.Result, &command); err != nil || command.OperationId != receipt.OperationId || command.Revision == nil || *command.Revision != "2" {
		t.Fatal("native direct command receipt incompatible", err)
	}
	encodedCommand, _ := json.Marshal(command)
	var canonical map[string]any
	_ = json.Unmarshal(encodedCommand, &canonical)
	if canonical["revision"] != "2" {
		t.Fatal("native command output must retain decimal string")
	}
	for _, raw := range []string{`""`, `0`, `2`, `18446744073709551615`, `"9007199254740993"`} {
		var value Uint64Decimal
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(raw, err)
		}
		encoded, _ := json.Marshal(value)
		if len(encoded) < 2 || encoded[0] != '"' {
			t.Fatal("revision output lost canonical string")
		}
	}
	for _, raw := range []string{`-1`, `1.0`, `1e2`, `null`, `"01"`, `18446744073709551616`, `"+1"`, `" 1"`} {
		var value Uint64Decimal
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Fatal("invalid revision accepted", raw)
		}
	}
}
