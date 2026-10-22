package telegram

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	tg "github.com/mymmrac/telego"
)

func TestUnpairedStartRequiresCurrentUnexpiredPayloadAndGivesPrivateGuidance(t *testing.T) {
	for _, text := range []string{"/start", "/start SYNTHETIC_wrong", "/start SYNTHETIC_old"} {
		t.Run(strings.Fields(text)[0]+strings.TrimPrefix(text, "/start"), func(t *testing.T) {
			b, f := testBridge(t, Host{})
			b.state.Enabled, b.state.Bot = true, "fixture_caelis_bot"
			b.nonce, b.expires = "SYNTHETIC_current", time.Now().Add(time.Minute)
			before := b.Status().PairURL
			b.input(t.Context(), f, message(1, 10, 20, text))
			if b.Status().Candidate != "" || b.Status().Paired || b.Status().PairURL != before || len(f.texts) != 1 || !strings.Contains(f.texts[0], "complete pairing link") || strings.Contains(f.texts[0], "SYNTHETIC_") {
				t.Fatal("unbound Start authorized or disclosed pairing state")
			}
			if _, err := b.Confirm(); err == nil {
				t.Fatal("ordinary Start was confirmable")
			}
		})
	}
}

type pairingPollFailure struct{ *fakeClient }

func (f pairingPollFailure) Updates(context.Context, int) ([]tg.Update, error) {
	return nil, &transportError{issue: "network"}
}

func TestPairingPollFailureIsDistinctFromReceivedUnboundStart(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.newClient = func(string) (client, error) { return pairingPollFailure{f}, nil }
	if _, err := b.Connect(t.Context(), "", false); err != nil {
		t.Fatal("synthetic setup failed")
	}
	deadline := time.Now().Add(time.Second)
	for b.Status().Issue != "network" {
		if time.Now().After(deadline) {
			t.Fatal("polling failure did not propagate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	s := b.Status()
	if s.Candidate != "" || s.Paired || s.PairURL == "" {
		t.Fatal("poll failure confused with a consumed or confirmed pairing")
	}
}

func TestPairPayloadStatusPropagationStillRequiresExplicitConfirmation(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.state.Enabled, b.state.Bot = true, "fixture_caelis_bot"
	b.nonce, b.expires = "SYNTHETIC_current", time.Now().Add(time.Minute)
	link, _ := url.Parse(b.Status().PairURL)
	if link.Query().Get("start") != b.nonce || link.Path != "/fixture_caelis_bot" {
		t.Fatal("status did not bind the generated link")
	}
	b.input(t.Context(), f, message(1, 10, 20, "/start "+link.Query().Get("start")))
	s := b.Status()
	if s.Paired || s.Candidate == "" || s.PairURL == "" {
		t.Fatal("matching Start skipped explicit confirmation")
	}
	b.input(t.Context(), f, message(2, 99, 88, "/start "+b.nonce))
	if b.candidate.From.ID != 20 {
		t.Fatal("another account replaced the candidate")
	}
	encoded, _ := json.Marshal(s)
	var dto map[string]any
	_ = json.Unmarshal(encoded, &dto)
	if dto["candidate"] != s.Candidate || dto["paired"] != false || dto["pairURL"] != s.PairURL {
		t.Fatal("candidate DTO did not propagate")
	}
	s, err := b.Confirm()
	if err != nil || !s.Paired || s.Owner == "" || s.Candidate != "" || s.PairURL != "" || b.nonce != "" {
		t.Fatal("confirmed DTO or nonce consumption failed")
	}
	if _, err := b.Confirm(); err == nil {
		t.Fatal("consumed payload remained confirmable")
	}
}

func TestExpiredPairPayloadAndPrivateChatBoundaries(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.state.Enabled, b.state.Bot = true, "fixture_caelis_bot"
	b.nonce, b.expires = "SYNTHETIC_expired", time.Now().Add(-time.Minute)
	b.input(t.Context(), f, message(1, 10, 20, "/start "+b.nonce))
	if b.Status().Candidate != "" || b.Status().PairURL != "" || b.Status().Issue != "pairing_expired" || len(f.texts) != 1 || strings.Contains(f.texts[0], b.nonce) {
		t.Fatal("expired Start accepted or disclosed")
	}
	b.expires = time.Now().Add(time.Minute)
	group := message(2, 10, 20, "/start "+b.nonce)
	group.Message.Chat.Type = "group"
	b.input(t.Context(), f, group)
	bot := message(3, 10, 20, "/start "+b.nonce)
	bot.Message.From.IsBot = true
	b.input(t.Context(), f, bot)
	if b.Status().Candidate != "" || len(f.texts) != 1 {
		t.Fatal("non-private or Bot sender accepted")
	}
	b.input(t.Context(), f, message(4, 10, 20, "/start "+b.nonce))
	b.expires = time.Now().Add(-time.Minute)
	if _, err := b.Confirm(); err == nil || b.Status().Paired {
		t.Fatal("expired candidate was authorized")
	}
}

func TestSyntheticPollerDistinguishesOrdinaryStartAndRenewedPairLink(t *testing.T) {
	b, f := testBridge(t, Host{Chinese: func() bool { return true }})
	first, err := b.Connect(t.Context(), "", false) // fake client/store only
	if err != nil || first.PairURL == "" {
		t.Fatal("synthetic connection did not expose a pair link")
	}
	initial, _ := url.Parse(first.PairURL)
	b.mu.Lock()
	remaining := time.Until(b.expires)
	b.mu.Unlock()
	if remaining < 14*time.Minute || remaining > 15*time.Minute {
		t.Fatal("pair-link lifetime changed")
	}
	wait := func(test func() bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for !test() {
			if time.Now().After(deadline) {
				t.Fatal("synthetic poller did not propagate state")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	f.updates <- []tg.Update{message(1, 10, 20, "/start")}
	wait(func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.texts) > 0 })
	if b.Status().Candidate != "" || b.Status().Issue != "" {
		t.Fatal("received plain Start was treated as a transport error or paired")
	}
	f.mu.Lock()
	hint := f.texts[0]
	f.mu.Unlock()
	if !strings.Contains(hint, "完整配对链接") || strings.Contains(hint, initial.Query().Get("start")) {
		t.Fatal("plain Start guidance leaked payload or omitted correct path")
	}
	second, err := b.Connect(t.Context(), "", false)
	if err != nil || second.PairURL == first.PairURL {
		t.Fatal("synthetic renewal did not replace the payload")
	}
	renewed, _ := url.Parse(second.PairURL)
	f.updates <- []tg.Update{message(2, 10, 20, "/start "+initial.Query().Get("start"))}
	// The old payload must be consumed without binding, even when its repeated
	// guidance is throttled. A reply is not an ingress acknowledgement.
	wait(func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.state.Offset == 3 && len(b.state.Ingress) == 0
	})
	if b.Status().Candidate != "" {
		t.Fatal("old attempt matched a renewed pairing")
	}
	f.updates <- []tg.Update{message(3, 10, 20, "/start "+renewed.Query().Get("start"))}
	wait(func() bool { return b.Status().Candidate != "" })
	if b.Status().Paired {
		t.Fatal("polling bypassed explicit account confirmation")
	}
	confirmed, err := b.Confirm()
	if err != nil || !confirmed.Paired || confirmed.PairURL != "" {
		t.Fatal("confirmed status did not propagate")
	}
}
