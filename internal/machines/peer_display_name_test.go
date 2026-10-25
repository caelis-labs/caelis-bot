package machines

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"testing"
)

func TestPeerAutomaticDisplayNameDoesNotChangeOriginalPairIntent(t *testing.T) {
	target := newDualPeerTarget(t)
	s, e := Open(t.TempDir(), &dualPeerLocal{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	code, e := target.peer.Pairing(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	input := api.BotConnectionInput{Address: target.http.URL, Code: code.Code}
	ref, e := s.PrepareBotConnection(input, nil)
	if e != nil {
		t.Fatal(e)
	}
	v, _, e := s.ConnectPreparedBot(t.Context(), ref, input)
	if e != nil {
		t.Fatal(e)
	}
	host, e := os.Hostname()
	if e != nil {
		t.Fatal(e)
	}
	if v.Name != host || s.Machines()[0].Name != host {
		t.Fatal("authenticated remote name not presented")
	}
	if s.state.Profiles[ref.ID].View.Name != input.Address {
		t.Fatal("display metadata changed persisted authorization intent")
	}
	if _, e = s.ReadBotConnectionReceipt(t.Context(), ref); e != nil {
		t.Fatal("automatic name trapped lost original reply", e)
	}
	// An explicit name is still user's display choice, not another peer identity.
	input.ID = ref.ID
	input.Code = ""
	input.Name = "My worker"
	if _, e = s.ConnectBot(t.Context(), input); e != nil {
		t.Fatal(e)
	}
	if s.Machines()[0].Name != "My worker" {
		t.Fatal("remote name overrode user rename")
	}
}
