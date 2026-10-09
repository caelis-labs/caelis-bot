package bot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestPrivateHandoffPersistsAcrossRestartAndConsumesOnlyAcceptedDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff-codex.json")
	h, err := openHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.save("call-1", "old-session", "Identity; goal; pending task original receipt."); err != nil {
		t.Fatal(err)
	}
	if err := h.save("call-2", "old-session", "overwrite"); err == nil {
		t.Fatal("overwrote unresolved original call")
	}
	h, err = openHandoff(path)
	if err != nil {
		t.Fatal(err)
	}
	seed := h.prepare()
	if !strings.Contains(seed.Text, "pending task original receipt") || seed.HandoffDigest == "" {
		t.Fatal("private handoff not restored")
	}
	if err := h.consume(api.ContextSeed{HandoffDigest: "different"}); err != nil {
		t.Fatal(err)
	}
	if h.prepare().HandoffDigest != seed.HandoffDigest {
		t.Fatal("wrong receipt consumed handoff")
	}
	if err := h.consume(seed); err != nil {
		t.Fatal(err)
	}
	h, err = openHandoff(path)
	if err != nil || h.prepare().Text != "" {
		t.Fatal("accepted handoff replayed", err)
	}
}
