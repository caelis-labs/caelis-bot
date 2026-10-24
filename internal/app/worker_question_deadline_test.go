package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

type questionDeadlineOwner struct {
	*workerFixture
	t       *testing.T
	maximum time.Duration
	calls   int
	request api.TaskMessage
}

func (o *questionDeadlineOwner) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	o.calls++
	o.request = in
	deadline, ok := ctx.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > o.maximum {
		o.t.Errorf("Worker answer inherited wrong deadline: %v remaining=%v maximum=%v", ok, remaining, o.maximum)
	}
	return api.Task{ID: in.ID, Status: "unknown", Outcome: "unknown"}, context.DeadlineExceeded
}
func TestWorkerAsyncAnswerUsesExistingRemoteAdmissionBudget(t *testing.T) {
	for _, mode := range []string{"remote", "earlier-parent", "local"} {
		t.Run(mode, func(t *testing.T) {
			machine := "peer-B"
			maximum := api.WorkerAdmissionTimeout
			if mode == "local" {
				machine = ""
				maximum = 10 * time.Minute
			}
			if mode == "earlier-parent" {
				maximum = time.Second
			}
			engine := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
			a, root := fixtureApp(t, engine, Host{})
			fixture := &workerFixture{runtime: "codex", states: map[string]api.WorkState{"original-work": {Task: api.Task{ID: "original-work", Machine: machine, Status: "working"}, Runtime: "codex", ExecutionKey: "original-turn", AsyncQuestions: []api.Item{{ID: "original-item", AsyncCallID: "original-call"}}}}}
			pool := workPool(root, "codex", map[string]*workerFixture{"codex": fixture})
			wireTestWorkers(t, a, pool)
			native := &questionDeadlineOwner{workerFixture: fixture, t: t, maximum: maximum}
			manager, e := tasks.Open(filepath.Join(root, "deadline-tasks.json"), filepath.Join(root, "Tasks"), "codex", native, fixture, engine.Snapshot)
			if e != nil {
				t.Fatal(e)
			}
			a.tasks = manager
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()
			if mode == "earlier-parent" {
				var earlier context.CancelFunc
				ctx, earlier = context.WithTimeout(ctx, time.Second)
				defer earlier()
			}
			in := textchannel.Inbound{ID: "original-user-answer", Channel: "desktop", Conversation: "local"}
			r, e := a.answerWorkerQuestion(ctx, in, "original-work", "codex", "original-item", "controlled answer")
			if !errors.Is(e, context.DeadlineExceeded) || r.ID != in.ID || r.Outcome != "unknown" || native.calls != 1 || native.request.ID != "original-work" || native.request.Prompt != "controlled answer" || native.request.RequestID == "" {
				t.Fatal("deadline changed original binding/effect", r, e, native.calls, native.request)
			}
			original := native.request.RequestID
			r, _ = a.answerWorkerQuestion(ctx, in, "original-work", "codex", "original-item", "controlled answer")
			if native.calls != 1 || r.Outcome != "unknown" || native.request.RequestID != original {
				t.Fatal("unknown answer was replayed", r, native.calls)
			}
			if machine != "" {
				cancelled, stop := context.WithCancel(t.Context())
				stop()
				fresh := in
				fresh.ID = "undispatched-new-answer"
				r, e = a.answerWorkerQuestion(cancelled, fresh, "original-work", "codex", "original-item", "controlled answer")
				var refused *api.WorkMessageRejection
				if r.Outcome != "rejected" || !errors.As(e, &refused) || native.calls != 1 {
					t.Fatal("unadmitted answer falsely became unknown", r, e, native.calls)
				}
				r, _ = a.answerWorkerQuestion(cancelled, in, "original-work", "codex", "original-item", "controlled answer")
				if r.Outcome != "unknown" || native.calls != 1 {
					t.Fatal("cancelled reconciliation erased original uncertainty", r, native.calls)
				}
			}
		})
	}
}
