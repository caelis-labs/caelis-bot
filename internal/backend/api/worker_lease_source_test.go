package api

import (
	"encoding/json"
	"testing"
)

func TestWorkerLeaseSourcePreservesLegacyEncoding(t *testing.T) {
	s := WorkDispatchSource{NodeID: "n", Backend: "codex", BindingID: "b", OperationID: "o", Kind: "user"}
	b, _ := json.Marshal(s)
	if string(b) != `{"NodeID":"n","Backend":"codex","BindingID":"b","OperationID":"o","Kind":"user"}` {
		t.Fatal(string(b))
	}
	s.Lease = WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: "n", Backend: "codex", Epoch: "e"}
	if s.Validate() != nil {
		t.Fatal("exact grant rejected")
	}
	s.Lease.SourceNodeID = "forged"
	if s.Validate() == nil {
		t.Fatal("foreign source granted")
	}
}
