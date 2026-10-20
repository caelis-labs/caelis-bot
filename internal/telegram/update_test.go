package telegram

import (
	"context"
	"testing"

	tg "github.com/mymmrac/telego"
)

func TestUpdateJoinsAndPreservesQueueAndUnknown(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "paused", true: "enabled"}[enabled], func(t *testing.T) {
			root := t.TempDir()
			b, _ := Open(root, Host{})
			b.state.Enabled, b.state.ChatID = enabled, 17
			b.state.Offset = 42
			b.state.Ingress = []tg.Update{{UpdateID: 41}}
			b.state.Inputs["original"] = "unknown"
			b.state.Messages["sent-original"] = delivery{IDs: []int{-1}}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			b.cancel, b.done = cancel, done
			go func() { <-ctx.Done(); close(done) }()
			if err := b.PrepareUpdate(); err != nil {
				t.Fatal(err)
			}
			if err := b.CloseForUpdate(); err != nil {
				t.Fatal(err)
			}
			next, err := Open(root, Host{})
			if err != nil || next.state.Enabled != enabled || next.state.Offset != 42 || len(next.state.Ingress) != 1 || next.state.Inputs["original"] != "unknown" || next.state.Messages["sent-original"].IDs[0] != -1 {
				t.Fatal("handoff altered original state", err)
			}
		})
	}
}

func TestUpdateDoesNotDiscardSecretAnswer(t *testing.T) {
	b, _ := Open(t.TempDir(), Host{})
	b.secretIngress[7] = "private"
	if b.PrepareUpdate() == nil || b.secretIngress[7] != "private" || b.updating {
		t.Fatal("secret interaction lost")
	}
}
