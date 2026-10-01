package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func TestNotebookStandbyUsesTargetPreferencesAndPreservesIdentity(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	prefs := nodeagent.ExecutionPreferences{Schema: 1, Revision: 1, Conversation: api.WorkExecutionSettings{Model: "luna", Effort: "medium"}, Worker: api.WorkExecutionSettings{Model: "worker"}}
	if err := localstate.Write(filepath.Join(directory, "execution.json"), prefs); err != nil {
		t.Fatal(err)
	}
	profile := nodeagent.NotebookOwnerProfile(directory)
	request := nodeagent.NotebookOwnerRequest{Action: "prepare", BotID: "portable-bot", Runtime: &nodeagent.OwnedRuntimeSettings{Backend: api.NodeCodex, Binary: "/target/codex"}}
	if err := prepareNotebookProfile(profile, "node-A", request); err != nil {
		t.Fatal(err)
	}
	var execution api.ExecutionSettings
	if err := readNotebookJSON(filepath.Join(profile, "execution.json"), &execution); err != nil || execution.Model != "luna" || execution.Effort != "medium" {
		t.Fatal(execution, err)
	}
	auth, err := os.ReadFile(filepath.Join(profile, "Product", "product.auth"))
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareNotebookProfile(profile, "node-A", request); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(profile, "Product", "product.auth"))
	if string(auth) != string(after) {
		t.Fatal("existing target product credential overwritten")
	}
	request.BotID = "another-bot"
	if err = prepareNotebookProfile(profile, "node-A", request); err == nil {
		t.Fatal("adopted another Bot profile")
	}
}
func TestNotebookStoppedProofRequiresActualProfileLock(t *testing.T) {
	profile := t.TempDir()
	state, err := notebookOwnerState(context.Background(), profile, "node-A", "bot-A")
	if err != nil || !state.Stopped {
		t.Fatal(state, err)
	}
	release, err := lockProfile(filepath.Join(profile, ".product-owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	state, err = notebookOwnerState(context.Background(), profile, "node-A", "bot-A")
	if err == nil || state.Stopped {
		t.Fatal("unobservable active owner was called stopped", state, err)
	}
}

func TestFreshNotebookSessionPreservesOldNativeStateAndMemory(t *testing.T) {
	profile := t.TempDir()
	if err := os.Mkdir(filepath.Join(profile, "Product"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := localstate.Write(filepath.Join(profile, "bot.json"), map[string]any{"version": 1, "personalVersion": 1, "id": "same-bot", "schedules": []any{}}); err != nil {
		t.Fatal(err)
	}
	original := []byte("original native binding")
	if err := os.WriteFile(filepath.Join(profile, "conversation.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(profile, "personal"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "personal", "memory.sqlite"), []byte("target-only old evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resetNotebookSession(profile, "original-start"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(profile, "conversation.json")); !os.IsNotExist(err) {
		t.Fatal("old binding still active")
	}
	saved, err := os.ReadFile(filepath.Join(profile, "Product", "retired-original-start", "conversation.json"))
	if err != nil || string(saved) != string(original) {
		t.Fatal("original binding lost", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "Product", "retired-original-start", "personal", "memory.sqlite")); err != nil {
		t.Fatal("original target Memory lost", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "personal")); !os.IsNotExist(err) {
		t.Fatal("old evidence reused in fresh session")
	}
}
