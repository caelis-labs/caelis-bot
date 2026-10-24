package websurface

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestBotAuthorizationRetirementAndRestartPreserveOldUncertainty(t *testing.T) {
	s := testServer(t)
	c := pairClient(t, s)
	owner := sessionIDFor(c)
	originalRef := &api.BotConnectionReference{ID: "profile", Proof: "original-proof", Pairing: "old", CodeDigest: "old-code-digest"}
	confirmedRef := *originalRef
	confirmedRef.Pairing = "new"
	s.operations["original-unknown"] = operation{Owner: owner, State: "complete", Error: "operation_unconfirmed", BotConnection: originalRef, AuthorizationBy: "new-confirmed"}
	s.operations["new-confirmed"] = operation{Owner: owner, State: "complete", BotAuthorization: true, BotConnection: &confirmedRef, AuthorizationParent: "original-unknown"}
	s.operations["host-interrupted"] = operation{Owner: owner, State: "pending", BotAuthorization: true, BotConnection: &confirmedRef}
	for n := 0; n < 300; n++ {
		s.operations[fmt.Sprintf("recent-%04d", n)] = operation{Owner: owner, State: "complete", At: 100}
	}
	if err := s.saveOperations(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.operations["new-confirmed"]; ok {
		t.Fatal("fixture did not retire confirmed authorization")
	}
	if err := s.retired.Close(); err != nil {
		t.Fatal(err)
	}
	s.retired = nil
	s.operations = nil
	if err := s.loadOperations(); err != nil {
		t.Fatal(err)
	}
	id, op, ok := s.botAuthorizationTailLocked("original-unknown", owner)
	if !ok || id != "new-confirmed" || !botAuthorizationConfirmed(op) {
		t.Fatal("confirmed reference not retained through SQLite reopen", id, op, ok)
	}
	old := s.operations["original-unknown"]
	if old.Error != "operation_unconfirmed" || old.BotConnection.Pairing != "old" {
		t.Fatal("old unknown rewritten", old)
	}
	interrupted := s.operations["host-interrupted"]
	if interrupted.Error != "operation_unconfirmed" || interrupted.State != "complete" || interrupted.BotConnection.Pairing != "new" {
		t.Fatal("interrupted authorization lost original reference", interrupted)
	}
	if _, _, ok := s.botAuthorizationTailLocked("original-unknown", "different-web-owner"); ok {
		t.Fatal("authorization leaked across Web owners")
	}
	data, err := os.ReadFile(filepath.Join(s.config.Directory, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]operation
	if json.Unmarshal(data, &saved) != nil || saved["original-unknown"].Error != "operation_unconfirmed" {
		t.Fatal("old uncertainty not durable")
	}
}
