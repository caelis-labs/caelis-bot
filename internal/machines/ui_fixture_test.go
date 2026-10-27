package machines

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in native UI acceptance target: real peer protocol and durable pending
// request, no Runtime factory, approval, credential or execution authority.
func TestRemoteConnectionUIFixture(t *testing.T) {
	root := os.Getenv("CAELIS_BOT_MACHINE_UI_FIXTURE")
	if root == "" {
		t.Skip("explicit native UI acceptance target")
	}
	peer, err := workerpeer.Open(filepath.Join(root, "target"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var pairs, unpairs atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == workerpeer.Prefix+"pair" {
			pairs.Add(1)
		}
		if r.URL.Path == workerpeer.Prefix+"unpair" {
			unpairs.Add(1)
		}
		peer.ServeHTTP(w, r)
	}))
	defer server.Close()
	data, _ := json.Marshal(map[string]string{"url": server.URL})
	if err = os.WriteFile(filepath.Join(root, "ready.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(8 * time.Minute)
	for {
		select {
		case <-deadline:
			t.Fatal("UI fixture was not finished")
		default:
		}
		if _, err = os.Stat(filepath.Join(root, "stop")); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("production peer HTTP requests: pair=%d unpair=%d; no approval or Runtime", pairs.Load(), unpairs.Load())
	if pairs.Load() != 1 || unpairs.Load() != 0 {
		t.Fatal("UI management unexpectedly repeated pairing or unpaired")
	}
}
