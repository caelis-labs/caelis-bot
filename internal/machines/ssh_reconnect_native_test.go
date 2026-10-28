//go:build !windows

package machines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/testsupport/sshd"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// Real OpenSSH + production connection service/peer handler. Only the native
// model/Runtime boundary is controlled; no external account or Fedora is used.
func TestNativeSSHReconnectRevalidatesOriginalAuthorityAndRuntime(t *testing.T) {
	ssh := sshd.New(t)
	peer := newDualPeerTarget(t)
	var destination atomic.Pointer[workerpeer.Server]
	destination.Store(peer.peer)
	var pairWrites, recoverWrites atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(b))
			if r.URL.Path == workerpeer.Prefix+"pair" {
				var in workerpeer.PairingRequest
				_ = json.Unmarshal(b, &in)
				if !in.Read {
					pairWrites.Add(1)
				}
			} else {
				var in workerpeer.Request
				_ = json.Unmarshal(b, &in)
				if in.Action == "recover" {
					recoverWrites.Add(1)
				}
			}
		}
		destination.Load().ServeHTTP(w, r)
	}))
	defer proxy.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(proxy.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	target := api.SSHTarget{Host: "original-fixture", Port: port}
	address := "ssh://original-fixture:" + portText
	root, local := t.TempDir(), &dualPeerLocal{}
	s, err := Open(root, local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	if err := os.MkdirAll(filepath.Join(root, "SSH"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SSH", "config"), []byte(ssh.Config(target.Host)), 0600); err != nil {
		t.Fatal(err)
	}
	var pid atomic.Int64
	var logMu sync.Mutex
	var phases []string
	emit := func(r diagnosticlog.Record) {
		if r.Component == "sshtransport" && r.Phase == "started" {
			pid.Store(int64(r.ProcessID))
		}
		if r.Component == "machines" {
			logMu.Lock()
			phases = append(phases, r.Phase)
			logMu.Unlock()
		}
	}
	s.SetDiagnostics(emit)
	machine, err := s.ConnectBot(t.Context(), api.BotConnectionInput{Name: "fixture", Address: address})
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.peer.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: peer.peer.Status().Requests[0].ID, Allow: true}); err != nil {
		t.Fatal(err)
	}
	machine, err = s.InspectMachine(t.Context(), machine.ID, "")
	if err != nil || machine.State != "ready" {
		t.Fatal("original approved connection not ready", err)
	}
	before := s.state.Profiles[machine.ID]
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", s, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.StartTask(t.Context(), api.TaskStart{Machine: machine.ID, Title: "fixture task", Prompt: "fixture", RequestID: "original-start"})
	if err != nil {
		t.Fatal("create original fixture task", err)
	}
	owner := peer.ownerFor(s, "codex")
	owner.connectFailure.Store(true)
	p, _ := os.FindProcess(int(pid.Load()))
	if p == nil || p.Kill() != nil {
		t.Fatal("kill only the fixture channel")
	}
	waitPeerTest(t, func() bool { return s.SSHConnectionStatus(target).State == "failed" })
	// The real background health loop (normal 30s cadence) must rebuild the
	// original transport, then report Runtime unavailability rather than ready.
	s.mu.Lock()
	current := s.state.Profiles[machine.ID]
	current.View.CheckedAt = 0
	s.state.Profiles[machine.ID] = current
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.observeMachineProfiles(ctx) }()
	defer func() { cancel(); <-done }()
	until := time.Now().Add(40 * time.Second)
	var setup bool
	for time.Now().Before(until) {
		s.mu.Lock()
		setup = s.state.Profiles[machine.ID].View.State == "setup"
		s.mu.Unlock()
		if setup {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	cancel()
	<-done
	// The deferred join can observe the already-closed channel safely.
	if !setup || s.SSHConnectionStatus(target).State != "ready" || s.TaskMachines()[0].Ready {
		t.Fatal("health recovery did not revalidate native Runtime readiness")
	}
	owner.connectFailure.Store(false)
	if v, err := s.InspectMachine(t.Context(), machine.ID, ""); err != nil || v.State != "ready" {
		t.Fatal("original Runtime recovery not reflected", err)
	}
	// Restart restoration uses canonical SSH + original proof/IDs, not a saved
	// ephemeral loopback port or a replacement pairing/task request.
	s.Close()
	s, err = Open(root, local)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDiagnostics(emit)
	if v, err := s.InspectMachine(t.Context(), machine.ID, ""); err != nil || v.State != "ready" {
		t.Fatal("restart restoration required manual connect", err)
	}
	if got, err := s.ReadWork(t.Context(), task.ID); err != nil || got.ID != task.ID {
		t.Fatal("original task became inaccessible after recovery", err)
	}
	other := newDualPeerTarget(t)
	destination.Store(other.peer)
	if v, _ := s.InspectMachine(t.Context(), machine.ID, ""); v.State != "offline" || v.Issue != "bot_peer_identity_changed" {
		t.Fatal("different SSH destination identity became ready")
	}
	destination.Store(peer.peer)
	if err := peer.peer.Revoke(s.connectionSource(before)); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.InspectMachine(t.Context(), machine.ID, ""); v.PairingStatus != "revoked" || len(s.TaskMachines()) != 0 {
		t.Fatal("revoked grant silently restored")
	}
	after := s.state.Profiles[machine.ID]
	owner.mu.Lock()
	starts := owner.starts
	owner.mu.Unlock()
	if after.Peer != before.Peer || after.PairingEpoch != before.PairingEpoch || s.state.Routes[task.ID] != machine.ID || s.state.Runtimes[task.ID] != "codex" || starts != 1 || pairWrites.Load() != 1 || recoverWrites.Load() != 0 || local.mutations.Load() != 0 {
		t.Fatal("recovery replayed native work/pairing or changed original identity")
	}
	if err := s.RemoveMachine(t.Context(), machine.ID); err != nil {
		t.Fatal(err)
	}
	if s.SSHConnectionStatus(target).State != "idle" {
		t.Fatal("deleted last reference retained reconnecting channel")
	}
	if _, err := s.InspectMachine(t.Context(), machine.ID, ""); err == nil {
		t.Fatal("deleted connection revived through health read")
	}
	logMu.Lock()
	defer logMu.Unlock()
	t.Log("real OpenSSH + production health/peer lifecycle:", phases, "; pairing writes=1, native starts=1, recover writes=0, original task/runtime retained; revoked/deleted boundaries preserved")
}

func TestNativeSSHReconnectReclaimsAbandonedAttemptsWithoutForgettingStop(t *testing.T) {
	ssh := sshd.New(t)
	s, err := Open(t.TempDir(), &dualPeerLocal{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.MkdirAll(filepath.Join(s.root, "SSH"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "SSH", "config"), []byte(ssh.Config("retained abandoned-*")), 0600); err != nil {
		t.Fatal(err)
	}
	start := func(host string) api.SSHTarget {
		t.Helper()
		target := api.SSHTarget{Host: host, Port: 9317}
		if _, err := s.StartSSHConnection(target); err != nil {
			t.Fatal("abandoned channels exhausted capacity", err)
		}
		waitPeerTest(t, func() bool { return s.SSHConnectionStatus(target).State == "ready" })
		return target
	}
	live := start("abandoned-live")
	retained := start("retained")
	address, _ := sshtransport.Address(retained)
	s.state.Profiles["retained"] = profile{Peer: workerpeer.Connection{Address: address}, View: api.Machine{ID: "retained", Address: address}}
	if err := s.StopSSHConnection(retained); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		target := start(fmt.Sprintf("abandoned-%d", i))
		if err := s.StopSSHConnection(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.peerAddress(t.Context(), address); err == nil || err.Error() != "ssh_stopped" {
		t.Fatal("capacity cleanup forgot referenced stop", err)
	}
	if s.SSHConnectionStatus(live).State != "ready" {
		t.Fatal("capacity cleanup interrupted another in-progress connection flow")
	}
	t.Log("20 abandoned native channels reclaimed; referenced explicit stop remained fenced")
}
