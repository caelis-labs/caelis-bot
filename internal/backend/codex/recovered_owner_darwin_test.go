//go:build darwin && cgo

package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveredOwnerAuditTokenRejectsStaleProcessVersion(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	conn, err := dialExisting(testContext(t), path)
	if err != nil {
		t.Fatal(err)
	}
	peer := conn.peer
	_ = conn.Close()
	if peer.PID != id.PID || !peer.HasToken || peer.AuditToken[7] == 0 {
		t.Fatal("controlled socket lacks a process-bound audit token")
	}
	stale := peer.AuditToken
	stale[7]++ // Same numeric PID, wrong process version.
	if err := stopRecoveredProcess(id, stale, true); err == nil {
		t.Fatal("kernel accepted a stale process version")
	}
	if err := stopRecoveredProcess(id, peer.AuditToken, false); err == nil {
		t.Fatal("missing stable target fell back to a numeric PID")
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("stale or missing audit token stopped the live owner")
	}
}

func TestRecoveredOwnerMissingAuditTokenPersistsUnconfirmedExit(t *testing.T) {
	_, path, id := controlledOwnerProcess(t)
	dir := t.TempDir()
	opts := SessionOptions{Directory: dir, StateFile: filepath.Join(dir, "binding.json")}
	seed := NewSession(opts)
	seed.binding.OwnerEndpoint, seed.binding.OwnerProcess = "unix://"+path, &id
	seed.binding.CapacityStopRequest = "original-stop-request"
	if err := seed.save(); err != nil {
		t.Fatal(err)
	}
	seed.cancelLife()
	s := NewSession(opts)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	owner, ok := s.client.owner.(*recoveredNativeOwner)
	if !ok || !owner.hasToken {
		t.Fatal("verified private owner lacks its stable target")
	}
	owner.hasToken = false // Simulate unavailable platform target at Close.
	if err := s.Close(testContext(t)); err == nil || !strings.Contains(err.Error(), "需在应用外") {
		t.Fatal("unconfirmed exit was hidden", err)
	}
	if privateOwnerLifecycle(id) != ownerAlive {
		t.Fatal("missing stable target sent a numeric PID signal")
	}
	reloaded := NewSession(opts)
	defer reloaded.cancelLife()
	if !reloaded.binding.OwnerClosing || reloaded.binding.OwnerUnavailable || reloaded.binding.OwnerProcess == nil || *reloaded.binding.OwnerProcess != id || reloaded.binding.CapacityStopRequest != "original-stop-request" {
		t.Fatal("external cleanup state or original stop receipt was not durable")
	}
	if _, err := os.Stat(filepath.Join(dir, "binding.json")); err != nil {
		t.Fatal(err)
	}
}
