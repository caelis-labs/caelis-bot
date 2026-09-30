package nodeagent

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type installationFixture struct {
	mu     sync.Mutex
	calls  int
	status runtimemanagement.Status
}

func (i *installationFixture) Manage(ctx context.Context, r runtimemanagement.Request) (runtimemanagement.Status, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if r.Action == "detect" {
		return runtimemanagement.Status{Runtime: r.Runtime, Outcome: "accepted", Installed: i.status.Installed, Version: i.status.Version}, nil
	}
	if r.Action == "resolve" {
		return i.status, nil
	}
	i.calls++
	i.status = runtimemanagement.Status{Runtime: r.Runtime, RequestID: r.RequestID, Outcome: "accepted", Installed: true, Version: r.Version}
	return i.status, nil
}
func agentFixture(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Directory: directory, NodeID: "node-test", Binaries: map[api.NodeBackend]string{api.NodeCodex: "/absent-codex", api.NodeCaelis: "/absent-caelis"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(t *testing.T, s *Service, id string) nodeplane.ManagementRequest {
	t.Helper()
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r := nodeplane.ManagementRequest{Guard: api.NodeEditGuard{NodeID: s.options.NodeID, Backend: api.NodeCodex, Revision: catalog.Revision}, Ref: api.NodeOperationRef{NodeID: s.options.NodeID, Backend: api.NodeCodex, OperationID: id}, Installation: &api.NodeInstallationChange{Action: api.NodeInstall, Version: "0.159.2"}}
	r.Ref.RequestDigest = RequestDigest(r)
	return r
}
func TestOriginalInstallationReceiptSurvivesReconnectAndRestart(t *testing.T) {
	s := agentFixture(t)
	installer := &installationFixture{}
	s.installation = installer
	r := request(t, s, "install-original")
	result, err := s.Manage(t.Context(), r)
	if err != nil || result.Outcome != api.NodeCommitted {
		t.Fatalf("%+v %v", result, err)
	}
	replay, err := s.Manage(t.Context(), r)
	if err != nil || replay != result || installer.calls != 1 {
		t.Fatalf("replayed installation %+v %v calls=%d", replay, err, installer.calls)
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	restarted.installation = installer
	restored, err := restarted.Reconcile(t.Context(), r.Ref)
	if err != nil || restored != result {
		t.Fatalf("original receipt lost %+v %v", restored, err)
	}
	changed := r
	copy := *r.Installation
	changed.Installation = &copy
	changed.Installation.Version = "0.153.4"
	changed.Ref.RequestDigest = RequestDigest(changed)
	denied, err := s.Manage(t.Context(), changed)
	if err != nil || denied.Outcome != api.NodeRejected || installer.calls != 1 {
		t.Fatalf("changed original intent dispatched %+v %v", denied, err)
	}
}
func TestPrivateFramedCatalogAndUnknownDelivery(t *testing.T) {
	s := agentFixture(t)
	installer := &installationFixture{}
	s.installation = installer
	local, remote := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- productrpc.ServeNativeStream(ctx, remote, remote, Handler(s), allowed) }()
	c, err := NewClient(s.options.NodeID, local)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	catalog, err := c.Catalog(t.Context())
	if err != nil || len(catalog.Nodes) != 1 {
		t.Fatalf("%+v %v", catalog, err)
	}
	r := request(t, s, "framed-install")
	result, err := c.Manage(t.Context(), r)
	if err != nil || result.Outcome != api.NodeCommitted {
		t.Fatalf("%+v %v", result, err)
	}
	recovered, err := c.Reconcile(t.Context(), r.Ref)
	if err != nil || recovered != result {
		t.Fatalf("%+v %v", recovered, err)
	}
	if _, err := c.ReadRuntimeProof(t.Context(), api.WorkTarget{NodeID: s.options.NodeID, Backend: "codex", Role: api.RoleBot}); err == nil {
		t.Fatal("installed executable created managed owner proof")
	}
	c.Close()
	if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	disconnected, err := c.Manage(t.Context(), r)
	if err == nil || disconnected.Outcome != api.NodeUnknown || disconnected.Ref != r.Ref {
		t.Fatal("disconnect lost original unknown receipt")
	}
}
func TestSharedHostAndWindowsOwnershipNeverBecomeEligible(t *testing.T) {
	s := agentFixture(t)
	s.options.Health = func(context.Context, api.NodeBackend) (NativeHealth, error) {
		return NativeHealth{AuthenticationKnown: true, Authenticated: true, HealthKnown: true, Healthy: true, ManagedOwner: true, Fenceable: true, BotEligible: true, SharedHost: true}, nil
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeplane.ValidateCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range catalog.Nodes[0].Runtimes {
		if runtime.Roles[0].Eligible || runtime.Roles[0].Reason != "shared-runtime-not-fenceable" {
			t.Fatal("shared Host advertised controlled Bot")
		}
	}
}
func TestJoinAndBootstrapRejectUntrustedInputs(t *testing.T) {
	args, err := JoinArgs(SSHConfig{Target: "user@100.64.1.2"}, "/tmp/helper", "/tmp/private", "/tmp/agent.sock")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{"StrictHostKeyChecking=yes", "ForwardAgent=no", "StreamLocalBindMask=0177", "StreamLocalBindUnlink=no", "-R /tmp/private/agent.sock:/tmp/agent.sock"} {
		if !strings.Contains(joined, required) {
			t.Fatal(required)
		}
	}
	if strings.Contains(joined, "0.0.0.0") || strings.Contains(joined, "accept-new") {
		t.Fatal("join weakened network trust")
	}
	if _, err := JoinArgs(SSHConfig{Target: "-oProxyCommand=bad"}, "/tmp/helper", "/tmp/private", "/tmp/agent.sock"); err == nil {
		t.Fatal("SSH option injection")
	}
	path := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(path, []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifact(Artifact{Path: path, Arch: "amd64", ExpectedSHA256: strings.Repeat("0", 64)}); err == nil {
		t.Fatal("unverified payload accepted")
	}
}

func TestResolvedJoinPreservesTrustAndDropsAmbientForwardings(t *testing.T) {
	effective := []byte("hostname 100.64.1.2\nuser existing-user\nport 2222\nuserknownhostsfile /tmp/private-known-hosts\nglobalknownhostsfile /etc/ssh/ssh_known_hosts\nidentityfile /tmp/Existing Key\nlocalforward 0.0.0.0:99 example.com:80\nremoteforward 0.0.0.0:9999 localhost:80\ncontrolmaster auto\nproxycommand none\n")
	config, err := SanitizedSSHConfiguration(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"hostname 100.64.1.2", "user existing-user", "port 2222", "userknownhostsfile /tmp/private-known-hosts", "identityfile \"/tmp/Existing Key\"", "StrictHostKeyChecking yes"} {
		if !strings.Contains(string(config), v) {
			t.Fatal(v)
		}
	}
	if strings.Contains(string(config), "localforward") || strings.Contains(string(config), "remoteforward") || strings.Contains(string(config), "controlmaster auto") {
		t.Fatal("ambient forwarding survived")
	}
	if _, err := SanitizedSSHConfiguration(append(effective, []byte("proxyjump private-hop\n")...)); err == nil {
		t.Fatal("unsupported proxy silently changed connection")
	}
}
