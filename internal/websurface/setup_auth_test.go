package websurface

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSetupChallengeIsSessionBoundAndNeverPersistedInReceipts(t *testing.T) {
	s := testServer(t)
	a, b := pairClient(t, s), pairClient(t, s)
	var calls atomic.Int32
	owner := ""
	s.register(action{Path: "/api/test/device", Function: func(ctx context.Context) (api.SetupState, error) {
		calls.Add(1)
		owner = api.SetupAccess(ctx)
		return api.SetupState{LoginPending: true, Login: &api.SetupLogin{ID: "attempt", State: "pending", UserCode: "PRIVATE_USER_CODE", VerificationURL: "https://auth.openai.com/codex/device"}}, nil
	}})
	body := map[string]any{"id": "original-device-operation", "args": []any{}}
	if w := (&client{s: s}).request("/api/test/device", body); w.Code != 401 {
		t.Fatal("unauthenticated login accepted")
	}
	w := a.request("/api/test/device", body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PRIVATE_USER_CODE") || owner == "native" || owner == "web:" {
		t.Fatal("authenticated owner not injected")
	}
	disk, e := os.ReadFile(filepath.Join(s.config.Directory, "operations.json"))
	if e != nil || strings.Contains(string(disk), "PRIVATE_USER_CODE") {
		t.Fatal("challenge persisted")
	}
	repeated := a.request("/api/test/device", body)
	if repeated.Code != 200 || strings.Contains(repeated.Body.String(), "PRIVATE_USER_CODE") || calls.Load() != 1 {
		t.Fatal("receipt replayed ephemeral authority or dispatched twice")
	}
	if w := b.request("/api/test/device", body); w.Code != 409 {
		t.Fatal("receipt crossed session")
	}
	var receipt map[string]any
	if json.Unmarshal(repeated.Body.Bytes(), &receipt) != nil {
		t.Fatal("invalid receipt")
	}
}
