package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
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

func TestSanitizedConfigAcceptedByActualOpenSSH(t *testing.T) {
	binary, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable")
	}
	cmd := exec.CommandContext(t.Context(), binary, "-F", "/dev/null", "-T", "-G", "fixture.invalid")
	effective, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	config, err := SanitizedSSHConfiguration(effective)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "resolved-config")
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), binary, "-F", path, "-T", "-G", "fixture.invalid").Output()
	if err != nil {
		t.Fatal("resolved native metadata not accepted by OpenSSH", err)
	}
	if !strings.Contains(string(out), "hostname fixture.invalid") || !strings.Contains(string(out), "stricthostkeychecking true") {
		t.Fatal("resolved target or strict trust changed")
	}
}

type lostInstallationFixture struct{ installationFixture }

func (i *lostInstallationFixture) Manage(ctx context.Context, r runtimemanagement.Request) (runtimemanagement.Status, error) {
	status, err := i.installationFixture.Manage(ctx, r)
	if r.Action == "install" {
		status.Outcome = "unknown"
		return status, errors.New("reply lost")
	}
	return status, err
}
func TestUnknownInstallationReconcilesOriginalAndClearsPending(t *testing.T) {
	s := agentFixture(t)
	installer := &lostInstallationFixture{}
	s.installation = installer
	r := request(t, s, "lost-installation")
	first, err := s.Manage(t.Context(), r)
	if err != nil || first.Outcome != api.NodeUnknown {
		t.Fatalf("%+v %v", first, err)
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.PendingOperations) != 1 || catalog.PendingOperations[0] != r.Ref {
		t.Fatalf("original unknown not recoverable %+v %v", catalog, err)
	}
	resolved, err := s.Reconcile(t.Context(), r.Ref)
	if err != nil || resolved.Outcome != api.NodeCommitted || installer.calls != 1 {
		t.Fatalf("original receipt unresolved %+v %v calls=%d", resolved, err, installer.calls)
	}
	catalog, err = s.Catalog(t.Context())
	if err != nil || len(catalog.PendingOperations) != 0 {
		t.Fatal("confirmed original remained pending")
	}
}

func TestNativeCodexConversationWorkerPreferencesAreSeparateAndRecoverable(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture unavailable")
	}
	s := agentFixture(t)
	body, err := os.ReadFile("testdata/codex_setup_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	body = append([]byte("#!"+python+"\n"), body[strings.Index(string(body), "\n")+1:]...)
	binary := filepath.Join(s.options.Directory, "fixture-codex")
	if err := os.WriteFile(binary, body, 0700); err != nil {
		t.Fatal(err)
	}
	s.options.Binaries[api.NodeCodex] = binary
	configuration := &CodexConfiguration{Directory: s.options.Directory, Binary: binary}
	s.options.Configurations = map[api.NodeBackend]NativeConfiguration{api.NodeCodex: configuration}
	s.options.Health = func(ctx context.Context, b api.NodeBackend) (NativeHealth, error) {
		if b == api.NodeCodex {
			return configuration.Health(ctx)
		}
		return NativeHealth{}, nil
	}
	view, err := s.Configuration(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || !view.ConfigurationAvailable || view.Conversation == nil || view.Worker == nil || view.Guard.Revision != "1" {
		t.Fatalf("native scoped config unavailable %+v %v", view, err)
	}
	r := nodeplane.ManagementRequest{Guard: view.Guard, Ref: api.NodeOperationRef{NodeID: s.options.NodeID, Backend: api.NodeCodex, OperationID: "conversation-selection"}, Change: &api.RuntimeConfigurationChange{Action: "conversation-model", ExpectedRevision: view.Guard.Revision, Selection: api.WorkExecutionSettings{Model: "fixture-model", Effort: "high"}}}
	r.Ref.RequestDigest = RequestDigest(r)
	result, err := s.Manage(t.Context(), r)
	if err != nil || result.Outcome != api.NodeCommitted {
		t.Fatalf("%+v %v", result, err)
	}
	after, err := s.Configuration(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || after.Conversation.Model != "fixture-model" || after.Conversation.Effort != "high" || after.Worker.Model != "" {
		t.Fatalf("scopes mixed %+v %v", after, err)
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Reconcile(t.Context(), r.Ref)
	if err != nil || restored != result {
		t.Fatalf("original config lost %+v %v", restored, err)
	}
	stale := r
	copy := *r.Change
	stale.Change = &copy
	stale.Ref.OperationID = "stale-worker-selection"
	stale.Change.Action = "worker-model"
	stale.Ref.RequestDigest = RequestDigest(stale)
	denied, err := s.Manage(t.Context(), stale)
	if err != nil || denied.Outcome != api.NodeConflicted {
		t.Fatalf("stale scope applied %+v %v", denied, err)
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "NATIVE_ONLY_FIXTURE_SECRET") {
		t.Fatal("native authentication material entered catalog")
	}
	if catalog.Nodes[0].Runtimes[0].Authentication != api.NodeAuthenticated || catalog.Nodes[0].Runtimes[0].Roles[0].Eligible {
		t.Fatal("native auth created uncontrolled Bot eligibility")
	}
}

type configurationRevisionFixture struct{ revision string }

func (c *configurationRevisionFixture) Read(context.Context) (api.RuntimeConfiguration, error) {
	return api.RuntimeConfiguration{Revision: c.revision}, nil
}
func (c *configurationRevisionFixture) Change(context.Context, nodeplane.ManagementRequest) (api.RuntimeMutationResult, error) {
	return api.RuntimeMutationResult{}, errors.New("not a mutation fixture")
}
func TestInstallationUsesDisplayedConfigurationGuard(t *testing.T) {
	s := agentFixture(t)
	installer := &installationFixture{status: runtimemanagement.Status{Installed: true, Version: "0.153.4"}}
	s.installation = installer
	config := &configurationRevisionFixture{revision: "7"}
	s.options.Configurations = map[api.NodeBackend]NativeConfiguration{api.NodeCodex: config}
	view, err := s.Configuration(t.Context(), s.options.NodeID, api.NodeCodex)
	if err != nil || !view.ConfigurationAvailable || view.Guard.Revision != "7" {
		t.Fatal(view, err)
	}
	r := nodeplane.ManagementRequest{Guard: view.Guard, Ref: api.NodeOperationRef{NodeID: s.options.NodeID, Backend: api.NodeCodex, OperationID: "guarded-update"}, Installation: &api.NodeInstallationChange{Action: api.NodeUpdate, Version: "0.159.2", ExpectedVersion: "0.153.4"}}
	r.Ref.RequestDigest = RequestDigest(r)
	result, err := s.Manage(t.Context(), r)
	if err != nil || result.Outcome != api.NodeCommitted || installer.calls != 1 {
		t.Fatalf("displayed guard falsely conflicted %+v %v", result, err)
	}
	stale := r
	stale.Ref.OperationID = "stale-update"
	config.revision = "8"
	stale.Ref.RequestDigest = RequestDigest(stale)
	denied, err := s.Manage(t.Context(), stale)
	if err != nil || denied.Outcome != api.NodeConflicted || installer.calls != 1 {
		t.Fatal("stale native configuration guard admitted")
	}
	stale.Ref.OperationID = "stale-installation-version"
	stale.Guard.Revision = "8"
	stale.Ref.RequestDigest = RequestDigest(stale)
	denied, err = s.Manage(t.Context(), stale)
	if err != nil || denied.Outcome != api.NodeConflicted || denied.Message != "installation-version-changed" || installer.calls != 1 {
		t.Fatal("stale installed version admitted")
	}
}
func TestUnknownJournalBlocksFreshIntentAfterRestartOnlySameBackend(t *testing.T) {
	s := agentFixture(t)
	r := request(t, s, "unresolved-original")
	path := s.operationPath(r.Ref.OperationID)
	original := operation{Schema: 1, Request: r, Phase: "intent", Receipt: api.NodeOperationReceipt{Ref: r.Ref, Outcome: api.NodeUnknown}}
	if err := writeState(path, original); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	installer := &installationFixture{}
	restarted.installation = installer
	fresh := request(t, restarted, "fresh-id-bypass")
	denied, err := restarted.Manage(t.Context(), fresh)
	if err != nil || denied.Outcome != api.NodeRejected || denied.Message != "original-operation-pending" || installer.calls != 0 {
		t.Fatalf("fresh ID bypassed original journal %+v %v", denied, err)
	}
	other := fresh
	other.Ref.OperationID = "other-runtime"
	other.Ref.Backend = api.NodeCaelis
	other.Guard.Backend = api.NodeCaelis
	other.Ref.RequestDigest = RequestDigest(other)
	accepted, err := restarted.Manage(t.Context(), other)
	if err != nil || accepted.Outcome != api.NodeCommitted || installer.calls != 1 {
		t.Fatalf("different runtime incorrectly fenced %+v %v", accepted, err)
	}
}

func TestUnverifiableOriginalJournalFailsClosed(t *testing.T) {
	s := agentFixture(t)
	installer := &installationFixture{}
	s.installation = installer
	r := request(t, s, "first-intent")
	path := s.operationPath(r.Ref.OperationID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken original journal"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog(t.Context()); err == nil {
		t.Fatal("unverifiable journal silently disappeared")
	}
	fresh := r
	fresh.Ref.OperationID = "fresh-after-corruption"
	result, err := s.Manage(t.Context(), fresh)
	if err == nil && result.Outcome != api.NodeUnknown && result.Outcome != api.NodeRejected {
		t.Fatalf("unverifiable original adopted fresh mutation %+v %v", result, err)
	}
	if installer.calls != 0 {
		t.Fatal("corrupt journal dispatched a fresh operation")
	}
}
