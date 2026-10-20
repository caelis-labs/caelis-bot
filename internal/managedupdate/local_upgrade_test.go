//go:build darwin || linux

package managedupdate

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Use the production private control socket and held installation lease, not
// the stopped-owner shortcut. Native Bot work/Close/Ready are explicit fixtures.
func liveInstallerOwner(t *testing.T) (*Manager, fixture) {
	t.Helper()
	// Darwin Unix sockets have a much shorter path limit than t.TempDir.
	dir, err := os.MkdirTemp("/tmp", "cb-up-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	m, f := fixtureManager(t, dir)
	serveInstallerOwner(t, m, f)
	return m, f
}
func serveInstallerOwner(t *testing.T, m *Manager, f fixture) {
	t.Helper()
	lease, err := acquireLease(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lease.Close() })
	listener, err := net.Listen("unix", filepath.Join(f.root, "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: m.handler()}
	t.Cleanup(func() { server.Close() })
	m.boot = Boot{ID: "original-live-boot", PID: 123, Identity: f.a.Manifest.Identity()}
	if err := m.Ready(m.boot); err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
}
func waitInstallerHandoff(t *testing.T, m *Manager) Handoff {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		h, err := m.Handoff("original-live-boot")
		if err != nil {
			t.Fatal(err)
		}
		if h.State == "requested" {
			return h
		}
	}
	t.Fatal("installer never reached existing owner's preflight")
	return Handoff{}
}
func TestLiveInstallerUpgradeWaitsForOriginalHandoffAndCandidateHealth(t *testing.T) {
	m, f := liveInstallerOwner(t)
	archive := filepath.Join(filepath.Dir(f.root), "b.tar.gz")
	result := make(chan error, 1)
	go func() {
		_, err := ScheduleLocalUpgrade(t.Context(), f.root, archive, f.b, f.trust, "linux-arm64")
		result <- err
	}()
	h := waitInstallerHandoff(t, m)
	if id, _ := selected(f.root); id != f.a.ID {
		t.Fatal("switched before preflight")
	}
	h.State = "busy"
	if err := m.FinishHandoff(h); err != nil {
		t.Fatal(err)
	}
	if err := <-result; Code(err) != "update_scheduled_waiting_idle" {
		t.Fatal(err)
	}
	if !m.ready || m.boot.ID != "original-live-boot" {
		t.Fatal("busy owner was stopped")
	}
	if id, _ := selected(f.root); id != f.a.ID {
		t.Fatal("busy work bypassed")
	}
	original := m.state.Transaction.ID
	receiptCount := len(m.state.Receipts)
	// A second explicit invocation reuses the already scheduled transaction.
	if _, err := ScheduleLocalUpgrade(t.Context(), f.root, archive, f.b, f.trust, "linux-arm64"); Code(err) != "update_scheduled_waiting_idle" {
		t.Fatal(err)
	}
	if m.state.Transaction.ID != original || len(m.state.Receipts) != receiptCount {
		t.Fatal("scheduled operation replayed")
	}
	// Only the original Bot's successful Close receipt allows this transition.
	h.State = "committed"
	if err := m.FinishHandoff(h); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	if err := m.switchRelease(); err != nil {
		t.Fatal(err)
	}
	m.boot = Boot{ID: "candidate-boot", PID: 124, Identity: f.b.Manifest.Identity()}
	m.ready = false
	m.mu.Unlock()
	go func() {
		_, err := ScheduleLocalUpgrade(t.Context(), f.root, archive, f.b, f.trust, "linux-arm64")
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatal("selection alone confirmed install", err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := m.Ready(Boot{ID: "candidate-boot", PID: 124, Identity: f.b.Manifest.Identity()}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("health not read back")
	}
	// Idempotent installer repair on an already healthy version does not wait.
	if state, err := ScheduleLocalUpgrade(t.Context(), f.root, archive, f.b, f.trust, "linux-arm64"); err != nil || state != UpgradeHealthy {
		t.Fatal(state, err)
	}
}

func TestLiveInstallerUpgradePreservesUnknownOwnership(t *testing.T) {
	m, f := liveInstallerOwner(t)
	result := make(chan error, 1)
	go func() {
		_, err := ScheduleLocalUpgrade(t.Context(), f.root, filepath.Join(filepath.Dir(f.root), "b.tar.gz"), f.b, f.trust, "linux-arm64")
		result <- err
	}()
	h := waitInstallerHandoff(t, m)
	h.State = "failed"
	if err := m.FinishHandoff(h); err != nil {
		t.Fatal(err)
	}
	if err := <-result; Code(err) != "update_unconfirmed" {
		t.Fatal(err)
	}
	if id, _ := selected(f.root); id != f.a.ID {
		t.Fatal("unknown owner bypassed")
	}
}

func TestLiveInstallerWaitCancellationDoesNotStopOrReplay(t *testing.T) {
	m, f := liveInstallerOwner(t)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := ScheduleLocalUpgrade(ctx, f.root, filepath.Join(filepath.Dir(f.root), "b.tar.gz"), f.b, f.trust, "linux-arm64")
		result <- err
	}()
	waitInstallerHandoff(t, m)
	cancel()
	if err := <-result; Code(err) != "update_scheduled_waiting_idle" {
		t.Fatal(err)
	}
	if id, _ := selected(f.root); id != f.a.ID || !m.ready {
		t.Fatal("waiting cancellation stopped owner")
	}
}

func TestStoppedInstallerStartsOriginalOwnerAndOnlyWaitsForExistingTransaction(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "healthy", false: "candidate_failed"}[healthy], func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "cb-stopped-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			old, f := fixtureManager(t, dir)
			old.Close()
			archive := filepath.Join(filepath.Dir(f.root), "b.tar.gz")
			for n := 0; n < 2; n++ {
				state, err := ScheduleLocalUpgrade(t.Context(), f.root, archive, f.b, f.trust, "linux-arm64")
				if err != nil || state != UpgradeScheduled {
					t.Fatal("stopped install claimed health", state, err)
				}
			}
			m, err := Open(t.Context(), f.root, "linux-arm64", f.trust)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			tx := m.state.Transaction.ID
			if len(m.state.Receipts) != 1 {
				t.Fatal("duplicate scheduling", len(m.state.Receipts))
			}
			if id, _ := selected(f.root); id != f.a.ID {
				t.Fatal("candidate selected before old service start")
			}
			result := make(chan error, 1)
			go func() { result <- AwaitLocalUpgrade(t.Context(), f.root, f.b) }()
			// A systemd Type=simple start can return before the socket exists.
			select {
			case err := <-result:
				t.Fatal("did not wait for original owner startup", err)
			case <-time.After(250 * time.Millisecond):
			}
			serveInstallerOwner(t, m, f)
			h := waitInstallerHandoff(t, m)
			h.State = "committed"
			if err := m.FinishHandoff(h); err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			err = m.switchRelease()
			m.boot = Boot{ID: "candidate-boot", PID: 124, Identity: f.b.Manifest.Identity()}
			m.ready = false
			m.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				t.Fatal("scheduled/selected mistaken for healthy", err)
			case <-time.After(250 * time.Millisecond):
			}
			if healthy {
				if err := m.Ready(Boot{ID: "candidate-boot", PID: 124, Identity: f.b.Manifest.Identity()}); err != nil {
					t.Fatal(err)
				}
			} else {
				m.mu.Lock()
				err = m.rollback()
				m.boot = Boot{ID: "restored-boot", PID: 125, Identity: f.a.Manifest.Identity()}
				m.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				if err := m.Ready(Boot{ID: "restored-boot", PID: 125, Identity: f.a.Manifest.Identity()}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-result:
				if healthy && err != nil || !healthy && Code(err) != "candidate_rolled_back" {
					t.Fatal("wrong completion", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("health read did not complete")
			}
			if m.state.Transaction.ID != tx || len(m.state.Receipts) != 1 {
				t.Fatal("wait replayed original upgrade")
			}
		})
	}
}
