package machines

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestOriginalPairReceiptRequiresSameProofAndBothPeerPins(t *testing.T) {
	target := newDualPeerTarget(t)
	service, _, _, machine, _ := dualPeerConnect(t, target)
	service.mu.Lock()
	original := service.state.Profiles[machine]
	ref := service.botConnectionReference(original, target.http.URL)
	service.mu.Unlock()
	var calls atomic.Int32
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(workerpeer.Response{Contract: 1, Peer: "different-peer"})
	}))
	defer wrong.Close()
	bad := ref
	bad.Address = wrong.URL
	bad.Proof = "wrong-original-proof"
	if _, err := service.ReadBotConnectionReceipt(t.Context(), bad); err == nil || calls.Load() != 0 {
		t.Fatal("unowned proof reached another peer", err, calls.Load())
	}
	for _, known := range []bool{true, false} {
		bad = ref
		bad.Address = wrong.URL
		if !known {
			bad.Peer = ""
		} // first-pair receipt must also retain a subsequently learned pin
		if _, err := service.ReadBotConnectionReceipt(t.Context(), bad); err == nil {
			t.Fatal("different peer certified original grant")
		}
		service.mu.Lock()
		same := reflect.DeepEqual(service.state.Profiles[machine], original)
		service.mu.Unlock()
		if !same {
			t.Fatal("mismatched envelope overwrote original peer/profile")
		}
	}
	target.peer.Revoke(service.state.ProfileID)
	if _, err := service.ReadBotConnectionReceipt(t.Context(), ref); err == nil {
		t.Fatal("revoked proof certified original grant")
	}
}
