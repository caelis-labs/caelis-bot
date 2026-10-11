package bot

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

type workerInteractionFixture struct {
	decision api.Decision
	answer   string
}

func (*workerInteractionFixture) WorkerApprovals() []api.Approval {
	return []api.Approval{{ID: "native-worker", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "once", Scope: "once"}}}}
}
func (f *workerInteractionFixture) DecideWorkerApproval(_ context.Context, d api.Decision) (api.Approval, error) {
	f.decision = d
	return api.Approval{ID: d.ID, Owner: "task", Status: "sent"}, nil
}
func (*workerInteractionFixture) WorkerQuestions() []api.WorkerQuestion {
	return []api.WorkerQuestion{{ID: "Q7", TaskID: "task", Title: "Choose", Options: []string{"甲", "乙"}}}
}
func (f *workerInteractionFixture) AnswerWorkerQuestion(_ context.Context, _, value, requestID string) (api.Receipt, error) {
	f.answer = value
	return api.Receipt{ID: requestID, Outcome: "accepted"}, nil
}

func TestBotWorkerInteractionToolPreservesNativeFields(t *testing.T) {
	r, _, _ := fixture(t)
	f := &workerInteractionFixture{}
	r.ConfigureWorkerInteractions(f)
	listed := invokeCompact(t, r, "bot_interactions", `{"request":{"type":"list"}}`)
	data := listed.StructuredContent["data"].(map[string]any)
	if len(data["approvals"].([]api.Approval)) != 1 || len(data["questions"].([]api.WorkerQuestion)) != 1 {
		t.Fatal("Worker interactions not offered to Bot", data)
	}
	invokeCompact(t, r, "bot_interactions", `{"request":{"type":"decide","approval":"native-worker","choice":"once","answers":{"reason":["read only"]}}}`)
	if f.decision.ID != "native-worker" || f.decision.Choice != "once" || f.decision.Answers["reason"][0] != "read only" {
		t.Fatalf("native Worker decision altered: %+v", f.decision)
	}
	invokeCompact(t, r, "bot_interactions", `{"request":{"type":"answer","question":"Q7","value":"2","requestId":"stable-worker-answer"}}`)
	if f.answer != "2" {
		t.Fatal("Worker answer missing", f.answer)
	}
	approved := false
	for _, name := range botpolicy.ApprovedTools() {
		approved = approved || name == "bot_interactions"
	}
	if !approved {
		t.Fatal("Guardian tool would itself wait for an unrelated Bot tool approval")
	}
}
