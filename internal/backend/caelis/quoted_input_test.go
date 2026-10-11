package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestQuotedCaelisInputReachesNativePromptAndProjectsOnlyBody(t *testing.T) {
	var modelText string
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		var request wire.ApplicationPromptRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		modelText = value(request.Input)
		writeFixture(w, wire.CommandResult{OperationId: "quoted-caelis", Outcome: "accepted"})
	})
	s.tools = &api.ToolConnection{PrepareContext: func(context.Context) (api.ContextSeed, error) { return api.ContextSeed{Text: "[private seed]\n"}, nil }}
	in := api.Submission{ID: "quoted-caelis", Text: "本次正文", Quoted: &api.QuotedMessage{Role: "user", Text: "旧问题", Excerpt: true}}
	if receipt, err := s.Submit(t.Context(), in, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	if modelText != "[private seed]\n<reference>\n旧问题\n</reference>\n\n本次正文" {
		t.Fatal(modelText)
	}
	if s.state.ContextInputs[in.ID] != len(in.ModelQuotePrefix())+len("[private seed]\n") {
		t.Fatal("visible input prefix not tracked")
	}
	s.state.Views[s.state.Session.SessionId].Items = append(s.state.Views[s.state.Session.SessionId].Items, api.Item{ID: "quoted-user", Kind: "user", RequestID: in.ID, Text: modelText})
	found := false
	for _, item := range s.Snapshot().Items {
		if item.ID == "quoted-user" {
			found = true
			if item.Text != in.Text {
				t.Fatalf("quoted wrapper leaked to chat: %q", item.Text)
			}
		}
	}
	if !found {
		t.Fatal("quoted user item not projected")
	}
}
