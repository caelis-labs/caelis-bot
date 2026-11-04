package weixin

import (
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTaskTitleFallsBackToLiteralTextWithoutNewDelivery(t *testing.T) {
	const title = "😀 对齐 _ACP_ SDK & 发布"
	b, err := Open(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	b.state.BotID, b.state.OwnerID = "bot", "owner"
	freshWindow(b, "input")
	item := api.Item{ID: "task-start:original", Kind: "assistant", Text: title, Status: "completed", SeenAt: b.now().UnixMicro(), Task: &api.TaskPresentation{ID: "original", Title: title, Status: "working"}}
	snapshot := api.Snapshot{Items: []api.Item{item}}
	b.observeDelivery(snapshot)
	if len(b.state.Delivery.Queue) != 1 || b.state.Delivery.Queue[0].Body != title {
		t.Fatalf("plain task projection: %#v", b.state.Delivery.Queue)
	}
	original := b.state.Delivery.Queue[0]
	// Existing frozen parts, counters and original receipts remain authoritative.
	b.state.Delivery.Queue[0].Parts = []string{"**legacy title**"}
	b.state.Outputs[outputKey(item)] = outbound{State: "accepted", MessageID: "original-receipt"}
	before := b.state.Outputs[outputKey(item)]
	window := b.state.Window
	item.Task.Status = "completed"
	snapshot.Items[0] = item
	b.observeDelivery(snapshot)
	if !reflect.DeepEqual(before, b.state.Outputs[outputKey(item)]) || !reflect.DeepEqual(window, b.state.Window) || len(b.state.Delivery.Queue) != 1 || b.state.Delivery.Queue[0].Key != original.Key || b.state.Delivery.Queue[0].Parts[0] != "**legacy title**" {
		t.Fatal("formatting changed an existing receipt, frozen part or budget")
	}
}

func TestTaskTitlePlainProtocolDeliveryPreservesBudgetAndReceipt(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "unknown"}[unknown], func(t *testing.T) {
			const title = "对齐 ACP SDK & 发布"
			f := newDeliveryFixture(t)
			if unknown {
				f.hook = func(string) string { return "{" }
			}
			f.add("task-start:original", title, resultPriority)
			f.snapshot.Items[0].Task = &api.TaskPresentation{ID: "original", Title: title, Status: "working"}
			f.poll()
			sent := f.sent()
			if len(sent) != 1 || sent[0] != title || f.used() != 1 {
				t.Fatalf("plain text protocol/budget: %q, %d", sent, f.used())
			}
			receipts := make(map[string]outbound, len(f.b.state.Outputs))
			for key, value := range f.b.state.Outputs {
				receipts[key] = value
			}
			f.snapshot.Items[0].Task.Status = "completed"
			f.reopen()
			f.poll()
			if len(f.sent()) != 1 || f.used() != 1 || !reflect.DeepEqual(receipts, f.b.state.Outputs) {
				t.Fatal("restart/status update redelivered task title or changed receipts")
			}
		})
	}
}
