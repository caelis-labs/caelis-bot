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

func TestUnreadableSeedDoesNotMarkNewContextInjected(t *testing.T) {
	readable := false
	reads := 0
	c := &api.ToolConnection{PrepareContext: func(context.Context) (api.ContextSeed, error) {
		reads++
		if !readable {
			return api.ContextSeed{}, errors.New("required memory unavailable")
		}
		return api.ContextSeed{Text: "identity; task-original; pending question; revoked scope; receipt-original", HandoffDigest: "original"}, nil
	}}
	s := State{}
	if text, err := s.Prepare(t.Context(), c, "original-input"); err == nil || text != "" || s.Pending != nil || s.Injected {
		t.Fatal("failed seed was frozen or consumed", s)
	}
	readable = true
	text, err := s.Prepare(t.Context(), c, "original-input")
	if err != nil || text == "" {
		t.Fatal(err)
	}
	readable = false
	if retry, err := s.Prepare(t.Context(), c, "original-input"); err != nil || retry != text || reads != 2 {
		t.Fatal("unknown original input reread changed memory", err, reads)
	}
	s.Resolve("original-input", "accepted")
	if err := s.Cleanup(c); err != nil {
		t.Fatal(err)
	}
	if text, err := s.Prepare(t.Context(), c, "later-input"); text != "" || err != nil || reads != 2 {
		t.Fatal("accepted seed was injected twice", err, reads)
	}
}
