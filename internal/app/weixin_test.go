package app

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSharedChannelSnapshotAlreadyExcludesBotHandledWorkerApprovals(t *testing.T) {
	worker := api.Approval{ID: "worker-request", Owner: "task", TaskID: "owned-worker", Status: "pending", Choices: []api.Choice{{ID: "allowOnce", Scope: "once"}}}
	manual := api.Approval{ID: "manual", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "allow", Scope: "once"}}}
	engine := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Connection: "ready", RuntimeOwner: "native", Approvals: []api.Approval{worker, manual}}}
	a, _ := fixtureApp(t, engine, Host{})
	before := a.Backend.Snapshot()
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if err := a.companion.ImportSystemReceipts([]api.Receipt{{ID: api.WorkerInteractionMessageID("approval", "worker-request"), Outcome: "accepted"}}); err != nil {
		t.Fatal(err)
	}
	weixin := a.Backend.Snapshot()
	after := a.Backend.Snapshot()
	if len(weixin.Approvals) != 1 || weixin.Approvals[0].ID != "manual" {
		t.Fatal("Bot-owned request became a phone action", weixin.Approvals)
	}
	if len(before.Approvals) != 2 || len(after.Approvals) != 1 || len(engine.snapshot.Approvals) != 2 || before.RuntimeOwner != after.RuntimeOwner || len(before.Items) != len(after.Items) {
		t.Fatal("Weixin changed shared Desktop/Telegram projection")
	}
}
