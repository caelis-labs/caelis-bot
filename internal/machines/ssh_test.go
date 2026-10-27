package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestSSHCanonicalAddressReusesOriginalProfileProof(t *testing.T) {
	s, e := Open(t.TempDir(), inert{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := s.PrepareBotConnection(api.BotConnectionInput{Address: "ssh://box:09317"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.PrepareBotConnection(api.BotConnectionInput{Address: "ssh://box:9317"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if a.ID != b.ID || a.Pairing != b.Pairing || a.Proof != b.Proof || len(s.Machines()) != 1 {
		t.Fatal("same SSH endpoint allocated another intent")
	}
}
