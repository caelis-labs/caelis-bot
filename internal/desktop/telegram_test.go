package desktop

import (
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/telegram"
)

func TestTelegramOpenerRetainsCompletePairLinkAndNativeFailure(t *testing.T) {
	const link = "https://t.me/fixture_caelis_bot?start=SYNTHETIC_pair-9&extra=preserved"
	for _, tc := range []struct {
		name  string
		pair  bool
		state telegram.Status
		want  string
	}{
		{"pairing", true, telegram.Status{Bot: "fixture_caelis_bot", PairURL: link}, link},
		{"connected", true, telegram.Status{Bot: "fixture_caelis_bot", Paired: true}, "https://t.me/fixture_caelis_bot"},
		{"BotFather", false, telegram.Status{}, "https://t.me/BotFather"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened := ""
			failure := errors.New("synthetic opener unavailable")
			err := openTelegramSetup(tc.state, tc.pair, func(url string) error { opened = url; return failure })
			if opened != tc.want || !errors.Is(err, failure) {
				t.Fatal("native destination or failure changed")
			}
		})
	}
	called := false
	if openTelegramSetup(telegram.Status{Bot: "fixture_caelis_bot"}, true, func(string) error { called = true; return nil }) == nil || called {
		t.Fatal("missing payload opened an unbound Bot URL")
	}
	if openTelegramSetup(telegram.Status{}, false, nil) == nil {
		t.Fatal("missing opener reported success")
	}
	s := &Service{openExternalURL: func(url string) error {
		if url != "https://t.me/BotFather" {
			t.Fatal("wrong native service destination")
		}
		return nil
	}}
	if err := s.OpenTelegramSetup(false); err != nil {
		t.Fatal(err)
	}
}
