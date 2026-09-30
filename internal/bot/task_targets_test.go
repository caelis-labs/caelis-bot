package bot

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

type targetTaskFixture struct {
	api.TaskProvider
	start   api.TaskStart
	targets int
}

func (f *targetTaskFixture) WorkTargets() []api.WorkTargetInfo {
	f.targets++
	return []api.WorkTargetInfo{{Target: api.WorkTarget{NodeID: "rocky", Backend: "caelis", Role: api.RoleWorker}, Label: "Test Worker", State: "candidate"}}
}
func (f *targetTaskFixture) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.start = in
	return api.Task{ID: "owned", Target: in.Target}, nil
}

func TestTaskTargetDiscoveryAndSchemaPreserveMachineChoice(t *testing.T) {
	r, _, _ := fixture(t)
	f := &targetTaskFixture{}
	if err := r.ConfigureTasks(f, nil); err != nil {
		t.Fatal(err)
	}
	out := r.CallTool(t.Context(), "bot_task_targets", json.RawMessage(`{}`))
	if out.IsError || f.targets != 1 {
		t.Fatal(out)
	}
	for _, forbidden := range []string{"credential", "ssh", "workspaceRoot", "bindingId"} {
		if strings.Contains(out.Content[0]["text"], forbidden) {
			t.Fatal("discovery leaked native connection details")
		}
	}
	for _, bad := range []string{`{"source":"user"}`, `null`, `{} {}`} {
		if !r.CallTool(t.Context(), "bot_task_targets", json.RawMessage(bad)).IsError {
			t.Fatal("discovery accepted authority/setup parameters", bad)
		}
	}
	out = r.CallTool(t.Context(), "bot_task_start", json.RawMessage(`{"requestId":"stable-request","title":"Task","prompt":"Work","target":{"nodeId":"rocky","backend":"caelis","role":"worker"}}`))
	if out.IsError || f.start.Target == nil || *f.start.Target != (api.WorkTarget{NodeID: "rocky", Backend: "caelis", Role: api.RoleWorker}) {
		t.Fatal("explicit target was lost", out)
	}
	if !slices.Contains(botpolicy.ApprovedTools(), "bot_task_targets") || slices.Contains(botpolicy.ApprovedTools(), "bot_task_start") {
		t.Fatal("discovery changed dispatch review policy")
	}
	for _, definition := range r.Definitions() {
		if definition.Name == "bot_task_start" && !strings.Contains(string(definition.InputSchema), `"target"`) {
			t.Fatal("target not discoverable in start schema")
		}
	}
}
