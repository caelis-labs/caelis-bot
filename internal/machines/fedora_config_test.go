package machines

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Non-billable acceptance of the user's chosen native SSH Host. Uses a fresh
// controller profile and never changes SSH config, remote accounts or models.
func TestFedoraSSHConfigConnection(t *testing.T) {
	host := os.Getenv("CAELIS_BOT_TEST_CONFIG_SSH")
	if host == "" {
		t.Skip("set CAELIS_BOT_TEST_CONFIG_SSH for native configured-Host acceptance")
	}
	helper := os.Getenv("CAELIS_BOT_REMOTE_HELPER_DIR")
	artifact := func(arch string) ([]byte, error) { return os.ReadFile(filepath.Join(helper, "linux-"+arch)) }
	s, err := Open(filepath.Join(t.TempDir(), "Application Support", "Machines %h"), inert{}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := s.SSHConfigHosts()
	if err != nil || !slices.Contains(hosts, host) {
		t.Fatal("configured Host not discovered", err)
	}
	input := api.MachineInput{Address: host, SSHConfig: true, User: "ignored-stale-user", Port: 1, Authentication: "key", PrivateKey: "/ignored-stale-key"}
	node, err := s.ConnectMachine(t.Context(), input)
	if err != nil || node.State != "trust" || !node.SSHConfig {
		t.Fatalf("host-key confirmation %s %s %v", node.State, node.Issue, err)
	}
	input.ID, input.TrustFingerprint = node.ID, node.Fingerprint
	node, err = s.ConnectMachine(t.Context(), input)
	if err != nil || node.State == "offline" || node.Issue != "" || !node.SSHConfig || node.User == input.User || node.Port == input.Port {
		t.Fatalf("configured-Host authentication %s %s %v", node.State, node.Issue, err)
	}
	if !slices.Contains(node.Available, "codex") || !slices.Contains(node.Available, "caelis") {
		t.Fatal("remote runtimes not discovered")
	}
	again, err := Open(s.root, inert{}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Machines(); len(got) != 1 || !got[0].SSHConfig || got[0].Address != host {
		t.Fatal("configured-Host route not preserved")
	}
	node, err = again.InspectMachine(t.Context(), node.ID, "codex")
	if err != nil || node.State != "ready" {
		t.Fatalf("native runtime after reopen %s %s %v", node.State, node.Issue, err)
	}
	if err := again.RemoveMachine(t.Context(), node.ID); err != nil {
		t.Fatal("disposable controller cleanup", err)
	}
	t.Log("configured Host discovery, fingerprint, native authentication, runtime detection and reopen passed; no model call")
}
