package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/care"
)

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
