package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/care"
)

func TestCareLoadFailureDoesNotPreventPersonalPreparationOrStart(t *testing.T) {
	for _, fault := range []string{"invalid-json", "invalid-rule", "unsupported-version", "unreadable", "directory"} {
		t.Run(fault, func(t *testing.T) {
			e := newTestEngine()
			var reported []error
			a, root := fixtureApp(t, e, Host{
				CareSample:  func() care.Sample { return care.Sample{} },
				ReportError: func(err error) { reported = append(reported, err) },
			})
			path := filepath.Join(root, "care-fixture.json")
			store, err := care.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Save(t.Context(), care.Rule{ID: "break", Label: "Break", On: "clock.minute", When: "true", Prompt: "Offer a break", TimeZone: "UTC"}, nil); err != nil {
				t.Fatal(err)
			}
			state := store.Snapshot()
			state.Activations = []care.Activation{{ID: "care-uncertain", RuleID: "break", Version: state.Rules[0].Version, Status: "unknown"}}
			state.Watermarks["clock.minute"] = time.Now()
			state.Attempts = []time.Time{time.Now()}
			switch fault {
			case "invalid-rule":
				state.Rules[0].When = "event."
			case "unsupported-version":
				state.Version = 99
			}
			original, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "invalid-json" {
				original = original[:len(original)-1]
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if fault == "unreadable" {
				if err := os.Chmod(path, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
				if _, err := os.ReadFile(path); err == nil {
					t.Skip("filesystem permits reading mode-000 files")
				}
			}
			if fault == "directory" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.PreparePersonal(); err != nil {
				t.Fatalf("care fault prevented personal preparation: %v", err)
			}
			if a.personal == nil || a.notebook == nil || a.skillPath == "" {
				t.Fatal("personal preparation was incomplete")
			}
			if len(reported) != 1 || !strings.Contains(a.companion.Status(), "care") {
				t.Fatal("care failure not reported", reported, a.companion.Status())
			}
			if err := a.Start(); err != nil {
				t.Fatalf("care fault prevented app start: %v", err)
			}
			waitSignal(t, e.connectSeen)
			if !strings.Contains(a.Backend.Snapshot().BotStatus, "care") {
				t.Fatal("care failure absent from the product snapshot")
			}
			for _, args := range []string{`{"operation":"list"}`, `{"operation":"remove","id":"break"}`, `{"operation":"save","id":"break"}`} {
				out := a.companion.CallTool(t.Context(), "bot_care", json.RawMessage(args))
				if !out.IsError || !strings.Contains(out.Content[0]["text"], "care") {
					t.Fatal("unavailable care tool hid its load failure", out)
				}
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if fault == "directory" {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 0 {
					t.Fatal("invalid care directory modified", err)
				}
			} else {
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(original, after) {
					t.Fatal("care journal was replaced or repaired implicitly", err)
				}
			}
		})
	}
}

func TestRegisteredCareAdapterUsesApplicationLifecycle(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{
		CareSample:  func() care.Sample { return care.Sample{} },
		CareSources: []care.Source{{Name: "repository.checks", Description: "Fixture collector", Fields: map[string]string{"failed": "integer"}}},
	})
	event := care.Event{Source: "repository.checks", At: time.Now(), Data: map[string]any{"failed": 2}}
	if err := a.PublishCareEvent(t.Context(), event); err == nil {
		t.Fatal("adapter published before app start")
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"operation":"save","id":"ci","label":"CI failures","on":"repository.checks","when":"event.failed > 0","prompt":"Check CI failures","timeZone":"UTC"}`)
	if out := a.companion.CallTool(t.Context(), "bot_care", args); out.IsError {
		t.Fatal(out)
	}
	if err := a.PublishCareEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	out := a.companion.CallTool(t.Context(), "bot_care", json.RawMessage(`{"operation":"list"}`))
	var listed struct {
		State   care.State
		Sources []care.Source
	}
	if out.IsError || json.Unmarshal([]byte(out.Content[0]["text"]), &listed) != nil {
		t.Fatal(out)
	}
	if len(listed.Sources) != 4 || len(listed.State.Activations) != 1 || listed.State.Activations[0].Status != "pending" {
		t.Fatal("registered adapter did not enter the production queue", listed)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.PublishCareEvent(t.Context(), event); err == nil {
		t.Fatal("adapter published after shutdown")
	}
}
