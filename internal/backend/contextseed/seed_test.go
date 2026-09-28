package contextseed

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestUnknownAndCleanupRecoveryNeverConsumeEarly(t *testing.T) {
	reads, consumed := 0, 0
	fail := true
	c := &api.ToolConnection{PrepareContext: func(context.Context) (api.ContextSeed, error) {
		reads++
		return api.ContextSeed{Text: "memory and handoff", HandoffDigest: "a"}, nil
	}, ConsumeContext: func(api.ContextSeed) error {
		if fail {
			return errors.New("busy")
		}
		consumed++
		return nil
	}}
	s := State{}
	if _, err := s.Prepare(t.Context(), c, "first"); err != nil {
		t.Fatal(err)
	}
	s.Resolve("first", "unknown")
	_ = s.Cleanup(c)
	if consumed != 0 || s.Injected {
		t.Fatal("consumed before acceptance")
	}
	bytes, _ := json.Marshal(s)
	var restored State
	json.Unmarshal(bytes, &restored)
	if _, err := restored.Prepare(t.Context(), c, "other"); err == nil {
		t.Fatal("replaced unknown delivery")
	}
	if _, err := restored.Prepare(t.Context(), c, "first"); err != nil || reads != 1 {
		t.Fatal("reread uncertain payload", err)
	}
	restored.Resolve("first", "accepted")
	if err := restored.Cleanup(c); err == nil || restored.Pending == nil {
		t.Fatal("lost failed cleanup")
	}
	fail = false
	if err := restored.Cleanup(c); err != nil || consumed != 1 {
		t.Fatal(err, consumed)
	}
	if text, err := restored.Prepare(t.Context(), c, "next"); text != "" || err != nil {
		t.Fatal("repeated session injection")
	}
}
