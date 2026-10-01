package nodeplane

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestCoordinatorSourceRoutePresentationRequiresBoundedEnrolledSources(t *testing.T) {
	ids := []string{"coordinator", "source", "local"}
	valid := []api.NodeCoordinatorSourceRoute{{SourceNodeID: "source", SSHDestination: "existing-user@source-known-alias"}}
	if err := ValidateCoordinatorSourceRoutes("coordinator", ids, valid); err != nil {
		t.Fatal(err)
	}
	for _, routes := range [][]api.NodeCoordinatorSourceRoute{
		{{SourceNodeID: "missing", SSHDestination: "alias"}},
		{{SourceNodeID: "coordinator", SSHDestination: "alias"}},
		{{SourceNodeID: "source", SSHDestination: "one"}, {SourceNodeID: "source", SSHDestination: "two"}},
		{{SourceNodeID: "source", SSHDestination: "alias\ncommand"}},
	} {
		if err := ValidateCoordinatorSourceRoutes("coordinator", ids, routes); err == nil {
			t.Fatal("invalid route accepted", routes)
		}
	}
	tooMany := []api.NodeCoordinatorSourceRoute{}
	for i := range 17 {
		id := fmt.Sprintf("source-%d", i)
		ids = append(ids, id)
		tooMany = append(tooMany, api.NodeCoordinatorSourceRoute{SourceNodeID: id, SSHDestination: "existing-alias"})
	}
	if err := ValidateCoordinatorSourceRoutes("coordinator", ids, tooMany); err == nil {
		t.Fatal("unbounded route list accepted")
	}
}

func configurationIntent(t *testing.T) ManagementRequest {
	t.Helper()
	r := ManagementRequest{
		Guard:  api.NodeEditGuard{NodeID: "local", Backend: api.NodeCaelis, Revision: "7"},
		Ref:    api.NodeOperationRef{NodeID: "local", Backend: api.NodeCaelis, OperationID: "original-op"},
		Change: &api.RuntimeConfigurationChange{Action: "create-role", ID: "reviewer", Description: "审查<&>🌍", ExpectedRevision: "7"},
	}
	digest, err := ManagementDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Ref.RequestDigest = digest
	return r
}

func TestManagementOriginalIntentCannotMoveOrChangeOnRecovery(t *testing.T) {
	original := configurationIntent(t)
	if err := ValidateManagementRequest(original); err != nil {
		t.Fatal(err)
	}
	// The UI has selected another node by the time the first operation responds.
	displayed := original.Guard
	displayed.NodeID = "other-node"
	if MatchesEdit(displayed, original.Guard) {
		t.Fatal("old result applied to selected node")
	}
	for _, change := range []func(*ManagementRequest){
		func(r *ManagementRequest) { r.Ref.NodeID = "other-node" },
		func(r *ManagementRequest) { r.Ref.Backend = api.NodeCodex },
		func(r *ManagementRequest) { r.Change.Description = "changed intent" },
		func(r *ManagementRequest) { r.Change.ExpectedRevision = "8" },
		func(r *ManagementRequest) { r.Installation = &api.NodeInstallationChange{Action: api.NodeInstall} },
	} {
		r := configurationIntent(t)
		change(&r)
		if err := ValidateManagementRequest(r); err == nil {
			t.Fatal("mutated original intent admitted")
		}
	}
	// A journal handle is separate from the payload digest. Recovery still uses
	// the original ID; the agent's journal must reject a mismatching stored ref.
	if !MatchesEdit(original.Guard, original.Guard) {
		t.Fatal("unchanged edit rejected")
	}
	displayed = original.Guard
	displayed.Revision = "8"
	if MatchesEdit(displayed, original.Guard) {
		t.Fatal("stale revision accepted")
	}
}

func TestManagementDigestCrossLanguageUnicodeVector(t *testing.T) {
	r := configurationIntent(t)
	const expected = "610b927a402fc536e2c071c988446f8433c704e61a29cab86bbd4c10daaeeb20"
	if r.Ref.RequestDigest != expected {
		t.Fatalf("digest vector: got %s", r.Ref.RequestDigest)
	}
	// Byte-length prefix prevents ambiguous delimiters and embedded UTF-8 from
	// producing another field sequence with the same serialized intent.
	r.Change.ID = "reviewer:12"
	digest, err := ManagementDigest(r)
	if err != nil || digest == expected {
		t.Fatalf("changed digest: %s %v", digest, err)
	}
}

func TestDelayedLeaseResponseCannotExtendAdmission(t *testing.T) {
	started := time.Now()
	l := Lease{BotID: "persistent-bot", NodeID: "local", Backend: api.NodeCodex, Epoch: "epoch-2", ExpiresAt: started.Add(24 * time.Hour), TTLMs: 60000}
	deadline, err := l.Deadline(started, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// The response is delayed longer than the safe lifetime. Its wall expiry
	// intentionally lies far in the future and must not keep admission open.
	received := started.Add(56 * time.Second)
	if deadline.After(received) || deadline.Sub(started) != 55*time.Second {
		t.Fatal("response delay renewed lease authority", deadline)
	}
	l.TTLMs = 60001
	if _, err := l.Deadline(started, 0); err == nil {
		t.Fatal("lease exceeded internal expiry")
	}
	l.TTLMs = 60000
	proof := RuntimeProof{NodeID: l.NodeID, Backend: l.Backend, Epoch: "epoch-1", Controllable: true}
	if proof.ValidateFor(l) == nil {
		t.Fatal("old native fence proved a new lease")
	}
}

func TestNotebookVersionRemainsMonotonicAcrossOwnerEpochs(t *testing.T) {
	old := SnapshotRef{BotID: "persistent-bot", Epoch: "epoch-1", Version: "9007199254740993", Digest: strings.Repeat("a", 64)}
	next := old
	next.Epoch, next.Version, next.Digest = "epoch-2", "9007199254740994", strings.Repeat("b", 64)
	if newer, err := NewerSnapshot(old, next); err != nil || !newer {
		t.Fatal("monotonic handoff rejected", err)
	}
	if newer, err := NewerSnapshot(next, old); err == nil || newer {
		t.Fatal("old snapshot resurrected")
	}
	conflict := next
	conflict.Digest = old.Digest
	if _, err := NewerSnapshot(next, conflict); err == nil {
		t.Fatal("version reused for changed content")
	}
	if newer, err := NewerSnapshot(next, next); err != nil || newer {
		t.Fatal("receipt replay treated as new publication", err)
	}
	for _, bad := range []string{"0", "01", "18446744073709551616", "1.0"} {
		invalid := next
		invalid.Version = bad
		if invalid.Validate() == nil {
			t.Fatal("invalid snapshot version", bad)
		}
	}
}

func TestCatalogDoesNotAdvertiseUnfencedOrUnavailableRoaming(t *testing.T) {
	c := api.NodeCatalog{Revision: "1", SelectedNodeID: "local", ActiveBotNodeID: "local", Nodes: []api.NodeInfo{{ID: "local", Label: "This machine", OS: api.NodeDarwin, Join: api.NodeLocal, Runtimes: []api.NodeRuntime{{Backend: api.NodeCodex, Authentication: api.NodeAuthenticated, Health: api.NodeHealthy, Roles: []api.NodeRoleCapability{{Role: api.RoleWorker, Eligible: true}}}}}}}
	if err := ValidateCatalog(c); err != nil {
		t.Fatal("direct local default required broker", err)
	}
	c.Broker = &api.NodeBroker{NodeID: "local", Reachable: true, AutomaticRoaming: true}
	if ValidateCatalog(c) == nil {
		t.Fatal("Worker-only node advertised Bot roaming")
	}
	c.Nodes[0].Runtimes[0].Roles = append(c.Nodes[0].Runtimes[0].Roles, api.NodeRoleCapability{Role: api.RoleBot, Eligible: true})
	if err := ValidateCatalog(c); err != nil {
		t.Fatal(err)
	}
	c.Broker.Reachable = false
	if ValidateCatalog(c) == nil {
		t.Fatal("unreachable broker advertised automatic roaming")
	}
	c.Broker.Reachable = true
	c.Nodes[0].OS = api.NodeWindows
	if ValidateCatalog(c) == nil {
		t.Fatal("Windows Codex advertised Bot ownership")
	}
}
