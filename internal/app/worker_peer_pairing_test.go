package app

import (
	"crypto/rand"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPeerOnlyListenerAcceptsRequestsWithoutOpeningWorkerExecution(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	a.peer.server.ConfigureReadiness(nil)
	a.WorkerPeerHandler(http.NotFoundHandler(), "http://127.0.0.1:29317")
	if err := a.startWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	v := a.WorkerSharing()
	_, port, err := net.SplitHostPort(v.Listen)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + port
	proof := rand.Text() + rand.Text()
	id := rand.Text()
	r, err := workerpeer.RequestPairing(t.Context(), base, id, "source", "Fixture", proof, a.peer.server.Status().Peer)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	if len(a.WorkerSharing().Requests) != 1 || len(a.peer.owners) != 0 {
		t.Fatal("pending created foreign execution")
	}
	if _, err = workerpeer.Call(t.Context(), workerpeer.Connection{Address: base, Token: proof}, workerpeer.Request{Action: "info"}); err == nil {
		t.Fatal("request granted access")
	}
	for _, path := range []string{"/", "/api/v1/conversation/snapshot"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("browser exposed", resp.Status)
		}
	}
	if err = a.DecideWorkerPairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err != nil {
		t.Fatal(err)
	}
	if len(a.WorkerSharing().Sources) != 1 {
		t.Fatal("decision not persisted")
	}
}

func TestBrokenPeerRegistryLeavesLocalApplicationAvailable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "WorkerPeers"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WorkerPeers", "peers.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine()
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return engine, nil }}, nil
	})
	if err != nil {
		t.Fatal("remote registry prevented local startup", err)
	}
	defer a.Close()
	if a.currentEngine() != engine || a.Backend == nil || a.localWork == nil {
		t.Fatal("local authority unavailable")
	}
	if a.WorkerSharing().Issue != "worker_peer_state_unavailable" {
		t.Fatal("missing isolated remote error")
	}
	h := a.WorkerPeerHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "http://127.0.0.1:29317")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 204 {
		t.Fatal("peer state blocked local HTTP")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", workerpeer.Prefix+"pair", nil))
	if w.Code != 503 {
		t.Fatal("bad registry was silently replaced")
	}
}
