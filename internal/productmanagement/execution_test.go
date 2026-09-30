package productmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type executionSourceFixture struct {
	calls, reads int
	err          error
	state        ExecutionState
	models       []api.ModelOption
}

func (f *executionSourceFixture) ReadModelSettings(context.Context) (ExecutionState, []api.ModelOption, error) {
	f.reads++
	return f.state, f.models, nil
}
func (f *executionSourceFixture) ApplyModelSettings(context.Context, string, string, Selection) error {
	f.calls++
	return f.err
}
func TestExecutionScopeClosedProjectionAndDefiniteRejections(t *testing.T) {
	scope := Scope{BotID: "bot", Generation: "generation"}
	f := &executionSourceFixture{state: ExecutionState{Conversation: api.ExecutionSettings{Model: "public/model", Effort: "high", ServiceTier: "priority", ApprovalMode: "full-access"}}, models: []api.ModelOption{{Model: "public/model", Efforts: []string{"high"}}}}
	c, err := NewExecution(scope, f)
	if err != nil {
		t.Fatal(err)
	}
	view, err := c.ExecutionSettings(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), "approvalMode") || strings.Contains(string(b), "full-access") || strings.Contains(string(b), "serviceTier\"") {
		t.Fatal("private policy leaked", string(b))
	}
	command := ExecutionCommand{Scope: scope, ID: "stable-1", Target: "conversation", ExpectedRevision: view.Revision, Selection: Selection{Model: "public/model", Effort: "high"}}
	bad := command
	bad.Generation = "other"
	r, _ := c.ChangeExecutionSettings(t.Context(), bad)
	if r.Outcome != "rejected" || f.calls != 0 {
		t.Fatal(r)
	}
	if _, err := c.ExecutionSettings(t.Context(), bad.Scope); err == nil || f.reads != 1 {
		t.Fatal("foreign read reached source")
	}
	for _, test := range []struct {
		err  error
		code string
	}{{ErrExecutionConflict, "execution-revision-conflict"}, {ErrExecutionInvalid, "invalid-model-selection"}, {ErrExecutionUnavailable, "execution-unavailable"}, {ErrExecutionCancelled, "cancelled-before-dispatch"}} {
		f.err = test.err
		r, err = c.ChangeExecutionSettings(t.Context(), command)
		if err != nil || r.Outcome != "rejected" || r.Code != test.code {
			t.Fatal(r, err)
		}
	}
	f.err = errors.New("api-key-private-provider-detail")
	r, err = c.ChangeExecutionSettings(t.Context(), command)
	if !errors.Is(err, f.err) || r.Outcome != "unknown" || r.Code != "native-operation-unresolved" {
		t.Fatal(r, err)
	}
	b, _ = json.Marshal(r)
	if strings.Contains(string(b), "api-key") {
		t.Fatal("source error leaked")
	}
	f.models[0].Model = strings.Repeat("m", 257)
	if _, err = c.ExecutionSettings(t.Context(), scope); err == nil {
		t.Fatal("unbounded native catalog accepted")
	}
}
func TestExecutionRevisionIncludesPreservedPolicyAndOptionalWork(t *testing.T) {
	initial := ExecutionState{Conversation: api.ExecutionSettings{Model: "model", Effort: "low", ApprovalMode: "ask"}}
	first := ExecutionRevision(initial)
	initial.Conversation.ApprovalMode = "read-only"
	if ExecutionRevision(initial) == first {
		t.Fatal("policy absent from CAS")
	}
	second := ExecutionRevision(initial)
	initial.Conversation.ServiceTier = "priority"
	if ExecutionRevision(initial) == second {
		t.Fatal("tier absent from CAS")
	}
	second = ExecutionRevision(initial)
	initial.Work = &api.WorkExecutionSettings{}
	if ExecutionRevision(initial) == second {
		t.Fatal("work ownership absent from CAS")
	}
	command := ExecutionCommand{ID: "stable", Target: "conversation", ExpectedRevision: first, Selection: Selection{Model: "x\ncredential"}}
	if ValidExecutionCommand(command) {
		t.Fatal("control bytes accepted")
	}
}
