package managedupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossPinUsesVerifiedPackageAndActualHostHealth(t *testing.T) {
	m, f := fixtureManager(t)
	next, data := testPackage(t, f.key, "0.17.1-qa.3", func(files map[string][]byte) {
		files["Helpers/caelis"] = append(files["Helpers/caelis"], []byte("next")...)
		files["Resources/Caelis-Runtime.json"], _ = json.Marshal(map[string]any{
			"version": "v0.73.0", "commit": strings.Repeat("c", 40),
			"targets": map[string]any{"linux-arm64": map[string]string{"binary_sha256": hash(files["Helpers/caelis"])}}})
	})
	archive := filepath.Join(t.TempDir(), "next.tar.gz")
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := stage(f.root, archive, next, f.trust, "linux-arm64"); err != nil {
		t.Fatal(err)
	}
	m.state.Offer = &next
	m.state.Status.OfferID = next.ID
	receipt, err := m.Command(Command{ID: "cross-pin-original", Action: "install", OfferID: next.ID, When: "idle"})
	if err != nil || receipt.TransactionID == "" {
		t.Fatal("verified new pin rejected", receipt, err)
	}
	m.boot = Boot{ID: "old", PID: 1, Identity: f.a.Manifest.Identity()}
	m.ready = true
	h, err := m.Handoff("old")
	if err != nil {
		t.Fatal(err)
	}
	h.State = "committed"
	if err = m.FinishHandoff(h); err != nil {
		t.Fatal(err)
	}
	if err = m.switchRelease(); err != nil {
		t.Fatal(err)
	}
	m.boot = Boot{ID: "new", PID: 2, Identity: next.Manifest.Identity()}
	m.ready = false
	report := m.boot
	report.Runtime = Runtime{State: "pending", BundledVersion: "v0.73.0", HostVersion: "v0.72.2", HostBuild: "old-build", HostInstance: "old-instance"}
	if err = m.Ready(report); err != nil {
		t.Fatal(err)
	}
	if status := m.Status(); status.State != "installed" || status.Runtime.State != "pending" || status.Runtime.HostVersion != "v0.72.2" {
		t.Fatal("package pin mistaken for running Host", status)
	}
	report.Runtime = Runtime{State: "ready", BundledVersion: "v0.73.0", HostVersion: "v0.73.0", HostBuild: "new-build", HostInstance: "new-instance"}
	if err = m.Ready(report); err != nil || m.Status().Runtime != report.Runtime {
		t.Fatal("idle replacement not observed", err)
	}
	again, err := m.Command(Command{ID: "cross-pin-original", Action: "install", OfferID: next.ID, When: "idle"})
	if err != nil || again != receipt {
		t.Fatal("original receipt changed", err)
	}
	report.ID = "wrong"
	if err = m.Ready(report); Code(err) != "boot_mismatch" {
		t.Fatal("foreign report accepted", err)
	}
}

func TestRuntimeFailureDoesNotConfirmCandidateOrDowngradeHost(t *testing.T) {
	m, f := fixtureManager(t)
	m.boot = Boot{ID: "candidate", PID: 2, Identity: f.b.Manifest.Identity()}
	m.state.Transaction = &transaction{ID: "original", Offer: f.b.ID, Previous: f.a.ID, Phase: "switched"}
	report := m.boot
	report.Runtime = Runtime{State: "failed", Reason: "runtime_start_failed"}
	if err := m.Ready(report); Code(err) != "runtime_start_failed" || m.ready || m.state.Transaction.Phase == "healthy" {
		t.Fatal("failed Host became successful update", err)
	}
	if err := m.rollback(); err != nil || m.Status().Reason != "runtime_start_failed" {
		t.Fatal("failure reason lost", err)
	}
	// rollback only changes the Bot selector; the launcher has no Core command.
	if id, _ := selected(f.root); id != f.a.ID {
		t.Fatal("Bot selector not restored")
	}
}
