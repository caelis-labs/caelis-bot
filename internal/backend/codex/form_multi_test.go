package codex

import (
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestNativeArrayEnumFormProjectsAndSubmitsMultipleValues(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "array-form")
	f.emit(wireMessage{ID: raw("array-form-request"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{
		"threadId": "thread-native", "turnId": "run-native", "mode": "form", "serverName": "fixture",
		"requestedSchema": map[string]any{"type": "object", "required": []string{"tags"}, "properties": map[string]any{
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"red", "green", "blue"}}, "minItems": 1, "maxItems": 2},
		}},
	})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	a := view.Approvals[0]
	if len(a.Questions) != 1 || !a.Questions[0].Multiple || !a.Questions[0].Required || len(a.Questions[0].Options) != 3 || a.Choices[0].ID != "accept" {
		t.Fatalf("array form projection: %+v", a)
	}
	if err := s.Decide(testContext(t), api.Decision{ID: a.ID, Choice: "accept", Answers: map[string][]string{"tags": {"red", "red"}}}); err == nil {
		t.Fatal("duplicate array values accepted")
	}
	if err := s.Decide(testContext(t), api.Decision{ID: a.ID, Choice: "accept", Answers: map[string][]string{"tags": {"red", "blue"}}}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"array-form-request"` || !strings.Contains(string(answer.Result), `"tags":["red","blue"]`) {
		t.Fatalf("native array response: %s", answer.Result)
	}
}

func TestNativePasswordFormFieldProjectsSecretMarker(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "password-form")
	f.emit(wireMessage{ID: raw("password-form-request"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{
		"threadId": "thread-native", "turnId": "run-native", "mode": "form", "serverName": "fixture",
		"requestedSchema": map[string]any{"type": "object", "required": []string{"credential"}, "properties": map[string]any{
			"credential": map[string]any{"type": "string", "format": "password", "writeOnly": true, "title": "Access code"},
		}},
	})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	a := view.Approvals[0]
	if len(a.Questions) != 1 || !a.Questions[0].Secret || a.Questions[0].Type != "string" || a.Choices[0].ID != "accept" {
		t.Fatalf("secret marker missing: %+v", a)
	}
	if err := s.Decide(testContext(t), api.Decision{ID: a.ID, Choice: "accept", Answers: map[string][]string{"credential": {"fixture-value"}}}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"password-form-request"` || !strings.Contains(string(answer.Result), `"credential":"fixture-value"`) {
		t.Fatal("native secret response was not bound to its original request")
	}
}
