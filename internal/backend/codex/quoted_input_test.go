package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestQuotedCodexInputReachesNativeTextAndProjectsOnlyBody(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	s.opts.BotTools = &api.ToolConnection{PrepareContext: func(context.Context) (api.ContextSeed, error) { return api.ContextSeed{Text: "[private seed]\n"}, nil }}
	in := api.Submission{ID: "quoted-codex", Text: "本次正文", Quoted: &api.QuotedMessage{Role: "assistant", Text: "旧回复", Excerpt: true}}
	native, err := s.prepareInput(in, nil, nil)
	if err != nil || len(native) != 1 {
		t.Fatal(native, err)
	}
	modelText := native[0]["text"].(string)
	if !strings.Contains(modelText, "<reference>\n旧回复\n</reference>") || !strings.HasSuffix(modelText, "本次正文") || !strings.Contains(modelText, "引用选段，非全文") {
		t.Fatal(modelText)
	}
	s.binding.ContextInputs = map[string]int{in.ID: len(in.ModelQuotePrefix())}
	native, err = s.prepareContextLocked(t.Context(), in.ID, native)
	if err != nil || s.binding.ContextInputs[in.ID] != len(in.ModelQuotePrefix())+len("[private seed]\n") {
		t.Fatal(err, s.binding.ContextInputs)
	}
	modelText = native[0]["text"].(string)
	if !strings.HasPrefix(modelText, "[private seed]\n") {
		t.Fatal(modelText)
	}
	s.applyItem("turn", nativeItem{ID: "user", ClientID: in.ID, Type: "userMessage", Content: []nativeInput{{Type: "text", Text: modelText}}}, true)
	visible := s.state.Items[s.items[opaque("turn", "user")]].Text
	if visible != in.Text {
		t.Fatalf("quoted wrapper leaked to chat: %q", visible)
	}
}
