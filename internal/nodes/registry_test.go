package nodes

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type runtimeFixture struct{ api.WorkRuntime }

func TestExplicitMachineBackendAndRoleDoNotFallback(t *testing.T) {
	local, remote := &runtimeFixture{}, &runtimeFixture{}
	r, err := New("codex", local)
	if err != nil {
		t.Fatal(err)
	}
	defaultTarget, err := r.ResolveWorkTarget(nil)
	if err != nil || defaultTarget != (api.WorkTarget{NodeID: "local", Backend: "codex", Role: api.RoleWorker}) {
		t.Fatal(defaultTarget, err)
	}
	target := api.WorkTarget{NodeID: "linux-a", Backend: "caelis", Role: api.RoleWorker}
	node := Node{ID: target.NodeID, Label: "Linux A", OS: "linux"}
	if err = r.Set(node, Capability{Target: target, State: Candidate}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ResolveWorkTarget(&target); err == nil {
		t.Fatal("candidate executed")
	}
	if err = r.Set(node, Capability{Target: target, State: Ready}, remote); err != nil {
		t.Fatal(err)
	}
	if got, err := r.WorkRuntimeFor(target); err != nil || got != remote {
		t.Fatal("wrong route", err)
	}
	for _, wrong := range []api.WorkTarget{{NodeID: "linux-b", Backend: "caelis", Role: api.RoleWorker}, {NodeID: "linux-a", Backend: "codex", Role: api.RoleWorker}, {NodeID: "linux-a", Backend: "caelis", Role: api.RoleBot}} {
		if _, err := r.ResolveWorkTarget(&wrong); err == nil {
			t.Fatal("target silently rerouted", wrong)
		}
	}
	if err = r.Set(node, Capability{Target: target, State: Unavailable}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.WorkRuntimeFor(target); err == nil {
		t.Fatal("unavailable route remained ready")
	}
	if routes := r.WorkRoutes(); len(routes) != 1 || routes[0].Runtime != local {
		t.Fatal(routes)
	}
}

func TestCapabilityRequiresMatchingIdentityAndReadiness(t *testing.T) {
	r, _ := New("codex", &runtimeFixture{})
	target := api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}
	for _, tc := range []struct {
		node  Node
		state Availability
		work  api.WorkRuntime
	}{
		{Node{ID: "other", Label: "Other"}, Ready, &runtimeFixture{}},
		{Node{ID: "remote", Label: "Remote"}, Ready, nil},
		{Node{ID: "remote", Label: "Remote"}, Candidate, &runtimeFixture{}},
	} {
		if err := r.Set(tc.node, Capability{Target: target, State: tc.state}, tc.work); err == nil {
			t.Fatal("invalid capability accepted")
		}
	}
}

func TestManagedDefaultAndExplicitLocalAreIndependent(t *testing.T) {
	managed, mac := &runtimeFixture{}, &runtimeFixture{}
	r, err := NewAt(Node{ID: "managed-linux", Label: "This machine", OS: "linux"}, "codex", managed)
	if err != nil {
		t.Fatal(err)
	}
	exactDefault := api.WorkTarget{NodeID: "managed-linux", Backend: "codex", Role: api.RoleWorker}
	if got, err := r.ResolveWorkTarget(nil); err != nil || got != exactDefault {
		t.Fatal(got, err)
	}
	macTarget := api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleWorker}
	if _, err := r.WorkRuntimeFor(macTarget); err == nil {
		t.Fatal("local alias fell back to managed machine")
	}
	if err := r.Set(Node{ID: api.LocalNodeID, Label: "Mac"}, Capability{Target: macTarget, State: Ready}, mac); err != nil {
		t.Fatal(err)
	}
	for target, want := range map[api.WorkTarget]api.WorkRuntime{exactDefault: managed, macTarget: mac} {
		if got, err := r.WorkRuntimeFor(target); err != nil || got != want {
			t.Fatal(target, err)
		}
	}
	if got, err := r.ResolveWorkTarget(nil); err != nil || got != exactDefault {
		t.Fatal("Mac registration changed default", got, err)
	}
	if err := r.Set(Node{ID: api.LocalNodeID, Label: "Mac"}, Capability{Target: macTarget, State: Unavailable}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveWorkTarget(&macTarget); err == nil {
		t.Fatal("detached Mac fell back to managed default")
	}
}

func TestNewAtRequiresCompleteMachineIdentity(t *testing.T) {
	for _, node := range []Node{{Label: "Machine"}, {ID: "managed"}} {
		if _, err := NewAt(node, "codex", &runtimeFixture{}); err == nil {
			t.Fatal("incomplete native identity accepted", node)
		}
	}
}
