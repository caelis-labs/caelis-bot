package app

import (
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestAddMachinePreparesPeerOnlyListenerAndRetainsAcrossHeadlessRestart(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	a.peer.server.ConfigureReadiness(nil)
	// The original headless/browser listener stays loopback and unchanged.
	a.WorkerPeerHandler(http.NotFoundHandler(), "http://127.0.0.1:29317")
	v, err := a.BeginWorkerPairing(t.Context())
	if err != nil || !v.Listening || v.Managed || len(v.Code) != 6 || len(v.Addresses) == 0 {
		t.Fatal(v, err)
	}
	_, port, err := net.SplitHostPort(v.Listen)
	if err != nil || port == "0" {
		t.Fatal(v.Listen, err)
	}
	base := "http://127.0.0.1:" + port
	for _, path := range []string{workerpeer.Prefix + "call", "/api/v1/conversation/snapshot", "/"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("peer listener exposed resident/browser or bypassed auth", path, resp.Status)
		}
	}
	saved, err := readPeerListener(filepath.Join(root, "WorkerPeers", "listener.json"))
	if err != nil || !saved.Dedicated || saved.Listen != v.Listen || saved.Address != v.Address {
		t.Fatal(saved, err)
	}
	if err := a.closeWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	a.peer.http = nil
	if err := a.startWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	if next := a.WorkerSharing(); next.Address != v.Address || next.Listen != v.Listen || next.Managed {
		t.Fatal("dedicated endpoint not restored", next)
	}
}
