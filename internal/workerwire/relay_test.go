package workerwire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRelayPreservesExactClosedFrameAndRejectsAuthorityChanges(t *testing.T) {
	pair := testPair()
	source := api.WorkDispatchSource{NodeID: pair.SourceNode, Backend: pair.SourceBackend, BindingID: "thread", OperationID: "turn", Kind: "native_activation"}
	valid := frame{Version: 1, ID: 1, Pair: pair, Method: "admission", Current: &source}
	encode := func(v frame) []byte {
		var b bytes.Buffer
		if err := writeFrame(&b, v); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	original := encode(valid)
	got, err := ReadRelayFrame(bytes.NewReader(original), pair, true)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("relay changed the original frame", err)
	}
	for _, name := range []string{"pair", "source-node", "source-backend", "source-kind", "unknown-method", "mixed-command", "target"} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			current := source
			changed.Current = &current
			switch name {
			case "pair":
				changed.Pair.BotID = "other-bot"
			case "source-node":
				current.NodeID = "other-host"
			case "source-backend":
				current.Backend = "caelis"
			case "source-kind":
				current.Kind = "generated"
			case "unknown-method":
				changed.Method = "shell"
			case "mixed-command":
				changed.Workspace = "/socket-authority"
			case "target":
				other := pair.Target
				other.NodeID = "other-node"
				changed.Method = "start"
				changed.Start = &api.WorkStart{TaskStart: api.TaskStart{Target: &other}}
				changed.Source = &current
			}
			if _, err := ReadRelayFrame(bytes.NewReader(encode(changed)), pair, true); err == nil {
				t.Fatal("authority change passed relay validation")
			}
		})
	}
	body, _ := json.Marshal(valid)
	body = append(body[:len(body)-1], []byte(`,"Socket":"/arbitrary"}`)...)
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(body)))
	if _, err := ReadRelayFrame(bytes.NewReader(append(prefix[:], body...)), pair, true); err == nil {
		t.Fatal("unknown socket field accepted")
	}
	binary.BigEndian.PutUint32(prefix[:], MaxRelayFrame+1)
	if _, err := ReadRelayFrame(bytes.NewReader(prefix[:]), pair, true); err == nil {
		t.Fatal("unbounded relay allocated a frame")
	}
	for _, field := range []string{"target", "source", "role"} {
		other := pair
		switch field {
		case "target":
			other.Target.Backend = "other"
		case "source":
			other.SourceBackend = "other"
		case "role":
			other.Target.Role = api.RoleBot
		}
		if ValidateRelayPair(other) == nil {
			t.Fatal("unsupported native pair admitted", field)
		}
	}
}

func TestRelayPreservesResponseAndRejectsRequestInReply(t *testing.T) {
	pair := testPair()
	for _, method := range []string{"", "state"} {
		var b bytes.Buffer
		if err := writeFrame(&b, frame{Version: 1, ID: 1, Pair: pair, Method: method}); err != nil {
			t.Fatal(err)
		}
		_, err := ReadRelayFrame(&b, pair, false)
		if (err == nil) != (method == "") {
			t.Fatal("reply method direction was not enforced", err)
		}
	}
}

func TestRelayPinsRawLeaseIdentityWithoutRewritingGrant(t *testing.T) {
	pair := testPair()
	pair.BotID = api.ProfileBotID("raw-origin")
	source := api.WorkDispatchSource{NodeID: pair.SourceNode, Backend: pair.SourceBackend, BindingID: "thread", OperationID: "turn", Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: "raw-origin", BrokerNodeID: "broker", SourceNodeID: pair.SourceNode, Backend: pair.SourceBackend, Epoch: "epoch"}}
	for _, rawID := range []string{"raw-origin", "foreign-origin", pair.BotID} {
		current := source
		current.Lease.BotID = rawID
		var b bytes.Buffer
		if err := writeFrame(&b, frame{Version: 1, ID: 1, Pair: pair, Method: "admission", Current: &current}); err != nil {
			t.Fatal(err)
		}
		original := append([]byte(nil), b.Bytes()...)
		got, err := ReadRelayFrame(&b, pair, true)
		if rawID == "raw-origin" {
			if err != nil || !bytes.Equal(got, original) {
				t.Fatal("exact raw grant was rewritten or rejected", err)
			}
		} else if err == nil {
			t.Fatal("foreign or already projected lease identity accepted", rawID)
		}
	}
}
