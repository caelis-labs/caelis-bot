package backend

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type presentationInitializer struct{ api.BotInitialization }

func (i presentationInitializer) Initialization() api.BotInitialization { return i.BotInitialization }
func (i presentationInitializer) Initialize(context.Context, api.BotIntroduction) (api.BotInitialization, error) {
	return i.BotInitialization, nil
}
func (i presentationInitializer) RetryInitialization(context.Context) (api.BotInitialization, error) {
	return i.BotInitialization, nil
}
func TestIntroductionPresentation(t *testing.T) {
	s := &Service{}
	s.initializer = presentationInitializer{api.BotInitialization{Status: "pending", RequestID: "intro-1", Prompt: "Your name is Test.", Message: "saved"}}
	v := api.Snapshot{Connection: "connecting", Message: "saved"}
	s.projectIntroduction(&v)
	if len(v.Items) != 1 || v.Items[0].RequestID != "intro-1" || !v.IntroductionPending || v.Message != "" {
		t.Fatalf("missing pending conversation: %+v", v)
	}
	s.projectIntroduction(&v)
	if len(v.Items) != 1 {
		t.Fatal("duplicated introduction")
	}
	v.Connection = "offline"
	v.Message = "failed"
	s.projectIntroduction(&v)
	if v.IntroductionPending || v.Message != "failed" {
		t.Fatal("hid real failure")
	}
	s.initializer = presentationInitializer{api.BotInitialization{Status: "accepted"}}
	v = api.Snapshot{Connection: "ready"}
	s.projectIntroduction(&v)
	if len(v.Items) != 0 || v.IntroductionPending {
		t.Fatal("replayed accepted intro")
	}
}
