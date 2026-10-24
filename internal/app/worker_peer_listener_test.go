package app

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestWorkerEndpointBindsPersistsAndPreservesPreviousOnConflict(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	first, err := a.SetWorkerSharing(true)
	if err != nil || !first.Listening || first.Listen == "127.0.0.1:0" {
		t.Fatal("actual initial listener", first, err)
	}
	assertAuth := func(address string) {
		t.Helper()
		resp, err := http.Post(address+workerpeer.Prefix+"call", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("listener bypassed peer auth", resp.Status)
		}
	}
	assertAuth(first.Address)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	wantConflict := "worker_listener_in_use"
	if runtime.GOOS == "windows" {
		// Inspect the actual Windows fixture's bind error. Exclusive-use or
		// access restrictions can produce WSAEACCES (10013), which is not proof
		// of an in-use address; WSAEADDRINUSE is 10048. Other platforms retain
		// the strict in-use assertion, and other Windows errors are failures.
		probe, probeErr := net.Listen("tcp", occupied.Addr().String())
		if probe != nil {
			probe.Close()
			t.Fatal("occupied fixture unexpectedly accepted another bind")
		}
		if errors.Is(probeErr, syscall.Errno(10013)) {
			wantConflict = "worker_listener_unavailable"
		} else if !errors.Is(probeErr, syscall.Errno(10048)) {
			t.Fatal("unexpected native Windows bind error", probeErr)
		}
		t.Log("native occupied-address bind:", probeErr)
	}
	if _, err = a.SetWorkerEndpoint(api.WorkerSharingInput{Listen: occupied.Addr().String()}); err == nil || err.Error() != wantConflict {
		t.Fatal("explicit port conflict", err)
	}
	if got := a.WorkerSharing(); got.Listen != first.Listen || got.Address != first.Address {
		t.Fatal("failed bind moved existing entry", got)
	}
	assertAuth(first.Address)
	next, err := a.SetWorkerEndpoint(api.WorkerSharingInput{Listen: "0.0.0.0:0", Address: "http://peer-network.example.test:23456"})
	if err != nil || next.Address != "http://peer-network.example.test:23456" || next.Listen == "0.0.0.0:0" {
		t.Fatal("controlled direct listening", next, err)
	}
	_, port, _ := net.SplitHostPort(next.Listen)
	assertAuth("http://127.0.0.1:" + port)
	saved, err := readPeerListener(filepath.Join(root, "WorkerPeers", "listener.json"))
	if err != nil || saved.Listen != next.Listen || saved.Address != next.Address {
		t.Fatal("restart endpoint publication", saved, err)
	}
	if err = a.closeWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	// Reopen the actual retained endpoint, without touching any Worker owner.
	a.peer.http = nil
	if err = a.startWorkerPeer(); err != nil {
		t.Fatal(err)
	}
	if got := a.WorkerSharing(); got.Listen != next.Listen || got.Address != next.Address {
		t.Fatal("restart changed published entry", got)
	}
	assertAuth("http://127.0.0.1:" + port)
}
func TestHeadlessWorkerEndpointPublishesWithoutCreatingAnotherListener(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	_ = a.WorkerPeerHandler(http.NotFoundHandler(), "http://127.0.0.1:29317")
	if _, err := a.SetWorkerEndpoint(api.WorkerSharingInput{Listen: "0.0.0.0:29317", Address: "http://host.example.test:29317"}); err == nil || err.Error() != "worker_listener_managed_headless" {
		t.Fatal(err)
	}
	v, err := a.SetWorkerEndpoint(api.WorkerSharingInput{Listen: "127.0.0.1:29317", Address: "http://127.0.0.1:49317"})
	if err != nil || v.Address != "http://127.0.0.1:49317" || !v.Managed || a.peer.http != nil {
		t.Fatal("headless did not retain existing listener", v, err)
	}
}
func TestWorkerListenerRequiresUsableExplicitPublication(t *testing.T) {
	for _, in := range []peerListenerConfig{{Listen: "0.0.0.0:0"}, {Listen: "[::]:0"}, {Listen: "127.0.0.1:65536"}, {Listen: "bad:23"}, {Listen: "127.0.0.1:0", Address: "http://0.0.0.0:9317"}, {Listen: "127.0.0.1:0", Address: "http://secret@127.0.0.1:9317"}} {
		if _, err := validatePeerListener(in); err == nil {
			t.Fatal("accepted unusable entry", in)
		}
	}
	c, err := readPeerListener(filepath.Join(t.TempDir(), "not-yet-created"))
	if err != nil || c.Listen != "127.0.0.1:0" {
		t.Fatal(c, err)
	}
	if _, err = validatePeerListener(peerListenerConfig{Listen: "127.0.0.1:0", Address: "http://127.0.0.1:29317"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerListenerPermissionErrorsDoNotClaimAddressInUse(t *testing.T) {
	for _, err := range []error{os.ErrPermission, os.NewSyscallError("bind", syscall.EACCES), errors.New("address already in use")} {
		if peerListenerAddressInUse(err) {
			t.Fatal("permission or error text claimed an in-use address", err)
		}
	}
}
