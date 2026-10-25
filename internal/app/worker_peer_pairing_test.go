package app

import (
	"bytes"
	"crypto/rand"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
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
	// This test owns a disposable root. Product diagnostics are deliberately
	// asynchronous/nonblocking on Close; use the existing synchronous logger so
	// the assertion's TempDir cleanup cannot race an unrelated diagnostic write.
	a, err := newApplication(root, Host{Diagnostics: diagnosticlog.New(filepath.Join(root, "Logs"))}, func(id string) (providerFactory, error) {
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

func TestRefreshPeerRetriesListenerWithoutReplacingIdentityOrOpeningOwner(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	identity := a.WorkerSharing().Peer
	a.peer.mu.Lock()
	a.peer.configurationErr = errors.New("temporary")
	a.peer.mu.Unlock()
	got := a.RefreshWorkerSharing(t.Context())
	if got.Issue != "" || !got.Listening || got.Peer != identity || len(a.peer.owners) != 0 {
		t.Fatal("listener not independently recovered", got)
	}
	original := a.peer.serverSnapshot()
	for range 3 {
		if next := a.RefreshWorkerSharing(t.Context()); next.Peer != identity || a.peer.serverSnapshot() != original {
			t.Fatal("refresh replaced healthy authority")
		}
	}
	if err := a.closeWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	if got := a.RefreshWorkerSharing(t.Context()); got.Listening {
		t.Fatal("closed App listener restarted")
	}
}

func TestUnsupportedRegistryIsNotMigratedAndRefreshCannotDiscardIt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "WorkerPeers")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "peers.json")
	original := []byte(`{"version":1,"identity":"old-unreleased","enabled":false,"grants":{},"bindings":{}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine()
	// This test owns a disposable root. Product diagnostics are deliberately
	// asynchronous/nonblocking on Close; use the existing synchronous logger so
	// the assertion's TempDir cleanup cannot race an unrelated diagnostic write.
	a, err := newApplication(root, Host{Diagnostics: diagnosticlog.New(filepath.Join(root, "Logs"))}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return engine, nil }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for range 2 {
		got := a.RefreshWorkerSharing(t.Context())
		if got.Issue != "worker_peer_state_unsupported" || got.Listening || a.currentEngine() != engine || len(a.peer.owners) != 0 {
			t.Fatal("bad state affected local authority or was guessed ready", got)
		}
		b, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(b, original) {
			t.Fatal("unsupported registry silently reset", err)
		}
	}
	// A separately restored original registry is re-read by explicit refresh; it
	// isn't permanently stuck behind the old server=nil projection.
	if err := os.WriteFile(path, []byte(`{"version":2,"identity":"restored-original","grants":{},"bindings":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got := a.RefreshWorkerSharing(t.Context())
	if got.Peer != "restored-original" || !got.Listening || got.Issue != "" {
		t.Fatal("restored original registry not reloaded", got)
	}
}

func TestClosedPeerRejectsCapturedHandlerAndLateFactory(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	handler := a.WorkerPeerHandler(http.NotFoundHandler(), "http://127.0.0.1:29317")
	if err := a.closeWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"identity", "pair"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", workerpeer.Prefix+path, nil))
		if w.Code != 503 {
			t.Fatal("closed mounted handler remained available", w.Code)
		}
	}
	if _, _, _, err := a.openForeignWorker(t.Context(), "source", "codex", t.TempDir()); err == nil || err.Error() != "worker_peer_closed" {
		t.Fatal("closed factory attempted native configuration", err)
	}
	if err := a.DecideWorkerPairing(t.Context(), api.WorkerPairingDecision{ID: "late", Allow: true}); err == nil {
		t.Fatal("closed host accepted decision")
	}
	if len(a.peer.owners) != 0 {
		t.Fatal("late native owner recreated")
	}
}
