package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func TestNodeRoamingInputReadsPersistedCoordinatorSourceRoutes(t *testing.T) {
	f := roamingControlFixture(t)
	path := filepath.Join(f.a.root, "nodeplane", "config.json")
	doc, err := loadNodeManagementDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.SourceRoutes = []nodeCoordinatorSourceRoute{{SourceNodeID: "node-fixture", CoordinatorNodeID: api.LocalNodeID, SSHDestination: "source-existing-local"}}
	if err := localstate.Write(path, doc); err != nil {
		t.Fatal(err)
	}
	in, err := f.c.input(t.Context(), controlRequest("enable-original"))
	if err != nil || len(in.SourceRoutes) != 1 || in.SourceRoutes[0].SourceNodeID != "node-fixture" || in.SourceRoutes[0].SSHDestination != "source-existing-local" {
		t.Fatal("private product routes did not reach native staging", in.SourceRoutes, err)
	}
	in.SourceRoutes[0].SSHDestination = "changed-draft"
	reloaded, err := loadNodeManagementDocument(path)
	if err != nil || reloaded.SourceRoutes[0].SSHDestination != "source-existing-local" {
		t.Fatal("stage input changed private pairing", err)
	}
}

type nativeLocalEnrollmentFixture struct {
	*nodeManagementFixture
	service *nodeagent.Service
}

func (f *nativeLocalEnrollmentFixture) NativeEnrollmentIdentity() (nodeagent.NativeEnrollmentIdentity, error) {
	return f.service.NativeEnrollmentIdentity()
}

func nativeLocalEnrollmentManagementFixture(t *testing.T) *nodeManagement {
	t.Helper()
	// This explicit Service enrollment fixture is outside the APP/source root.
	// The plan must use its actual stored identity, never fabricate node.json.
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	service, err := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: "actual-native-local", Join: api.NodeLocal})
	if err != nil {
		t.Fatal(err)
	}
	agent := &nativeNodeManagement{local: &nativeLocalEnrollmentFixture{singleNodeFixture(api.LocalNodeID), service}}
	return NewNodeManagement(agent, agent).(*nodeManagement)
}

func sealNativeRecoveryFixture(t *testing.T, p *roamingNativePlan) {
	t.Helper()
	identity, err := (&roamingNativeAssembly{original: nativeLocalEnrollmentManagementFixture(t)}).coordinatorEnrollmentIdentity(p.Coordinator)
	if err != nil {
		t.Fatal(err)
	}
	p.CoordinatorIdentity = identity
	p.SealVersion = 1
	for i := range p.Nodes {
		if m := p.Nodes[i].Plan.Managed; m != nil {
			copy := *m
			copy.CoordinatorIdentity = &identity
			p.Nodes[i].Plan.Managed = &copy
		}
	}
	p.ID, err = nativeRoamingPlanDigest(*p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range p.Nodes {
		p.Nodes[i].Plan.PlanID = p.ID
	}
}

func TestNativeFrozenPlanUsesActualLocalIdentityAndRejectsTamperOrDowngrade(t *testing.T) {
	a := nativeManagementApplication(t)
	helper := filepath.Join(a.root, "host")
	if err := os.WriteFile(helper, []byte("verified native fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	n := &roamingNativeAssembly{app: a, original: nativeLocalEnrollmentManagementFixture(t), options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }}}
	local := NodeRegistration{ID: api.LocalNodeID, Label: "Local", Join: api.NodeLocal}
	in := NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "enable-original", Coordinator: local, Nodes: []NodeRegistration{local}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}
	review, err := n.prepare(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	p := n.plans[review.ID]
	if p.CoordinatorIdentity.NodeID != "actual-native-local" || p.CoordinatorIdentity.Directory == p.Coordinator.Directory || p.Nodes[0].Plan.Managed.BrokerNodeID != api.LocalNodeID {
		t.Fatal("public local alias or roaming directory replaced actual enrollment", p.CoordinatorIdentity)
	}
	recordDirectory := t.TempDir()
	if err := os.Chmod(recordDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(recordDirectory, "roaming-deployment.json")
	write := func(p roamingNativePlan) {
		t.Helper()
		if err := nodeagent.WriteManagedPrivateJSON(filename, p); err != nil {
			t.Fatal(err)
		}
	}
	write(p)
	if _, err := readNativeRoamingPlan(filename); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*roamingNativePlan){
		func(p *roamingNativePlan) { p.SealVersion = 0 },
		func(p *roamingNativePlan) { p.CoordinatorIdentity.Directory = "/unpaired/private-directory" },
		func(p *roamingNativePlan) { p.Nodes[0].Plan.Managed.JoinSSHDestination = "unreviewed-alias" },
		func(p *roamingNativePlan) {
			p.Nodes[0].RuntimeBindings = append(p.Nodes[0].RuntimeBindings, NodeRoamingWorkerRuntime{Backend: "caelis", Binary: "/unreviewed/host", Store: "/unreviewed/store"})
		},
		func(p *roamingNativePlan) { p.Nodes[0].Preferences.Conversation.Model = "unreviewed-model" },
	} {
		wire, _ := json.Marshal(p)
		var changed roamingNativePlan
		if err := json.Unmarshal(wire, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(&changed)
		write(changed)
		before, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readNativeRoamingPlan(filename); err == nil {
			t.Fatal("unsealed or changed original plan was adopted")
		}
		after, err := os.ReadFile(filename)
		if err != nil || string(before) != string(after) {
			t.Fatal("unknown original record was replaced", err)
		}
	}
	p.Phase = "owners-ready"
	p.DisableOperationID = "disable-original"
	p.UnavailableCaelisWorkers = map[string]bool{"local": true}
	write(p)
	if _, err := readNativeRoamingPlan(filename); err != nil {
		t.Fatal("lifecycle facts changed immutable route seal", err)
	}
	// Losing the enrolled native port cannot silently create an identity file.
	n.original = nil
	if _, err := n.prepare(t.Context(), in); err == nil {
		t.Fatal("missing native enrollment port was inferred from local alias")
	}
}
