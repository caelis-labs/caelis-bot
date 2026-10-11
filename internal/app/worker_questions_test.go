package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestWorkerAsyncQuestionReachesGuardianAndAnswersOriginalWork(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
	a, root := fixtureApp(t, e, Host{})
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	manager, err := tasks.Open(filepath.Join(root, "task-questions.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.localWork, a.tasks = pool, manager
	work, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "worker-start-123", Title: "问问题", Prompt: "请提问", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	state := owner.states[work.ID]
	item := api.Item{ID: "worker-question:native", TurnKey: "worker-turn", Kind: "assistant", Status: "completed", AsyncCallID: "native-worker-call", AsyncQuestions: []api.AsyncQuestion{{Title: "请选择", Options: []string{"甲", "乙"}}}}
	state.AsyncQuestions = []api.Item{item}
	owner.states[work.ID] = state
	a.observeWorkerQuestions(t.Context())
	a.observeWorkerQuestions(t.Context())
	questions := a.WorkerQuestions()
	if len(questions) != 1 || questions[0].ID != "Q1" || questions[0].TaskID != work.ID || questions[0].Options[1] != "乙" || len(e.reports) != 1 || !strings.Contains(e.reports[0].Text, "Q1") {
		t.Fatalf("Worker question did not reach Bot once: %+v reports=%+v", questions, e.reports)
	}
	for _, v := range a.Backend.ChatSnapshot(0, "").Snapshot.Items {
		if v.ID == item.ID || strings.Contains(v.Text, "[Q1]") {
			t.Fatal("raw Worker question bypassed Bot into user chat", v)
		}
	}
	feedback := a.textControl.Handle(t.Context(), textchannel.Inbound{Channel: "telegram", Conversation: "paired", ID: "tg-worker-answer", Text: "/answer Q1 2"}, api.Snapshot{Connection: "offline"})
	if !strings.Contains(feedback, "已提交") || owner.sends != 1 || !strings.Contains(owner.lastPrompt, `"answer":"乙"`) || !strings.Contains(owner.lastPrompt, `"questionItemId":"[\"request_user_input_async\",\"native-worker-call\",0]"`) {
		t.Fatalf("original Worker did not receive exact answer: %q sends=%d prompt=%q", feedback, owner.sends, owner.lastPrompt)
	}
	duplicate := a.textControl.Handle(t.Context(), textchannel.Inbound{Channel: "weixin", Conversation: "paired", ID: "wx-stale-answer", Text: "/answer Q1 2"}, api.Snapshot{Connection: "offline"})
	if !strings.Contains(duplicate, "已提交") || owner.sends != 1 {
		t.Fatal("duplicate Worker answer dispatched", duplicate, owner.sends)
	}
	for _, v := range a.Backend.ChatSnapshot(0, "").Snapshot.Items {
		if v.ID == item.ID || strings.Contains(v.Text, "[Q1]") {
			t.Fatal("answered Worker question leaked into user chat", v)
		}
	}
}

func TestChangedWorkerQuestionGenerationGetsNewPrivateNotice(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
	a, root := fixtureApp(t, e, Host{})
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	a.localWork = pool
	item := api.Item{ID: "worker-question:changed", TurnKey: "worker-turn", Kind: "assistant", AsyncCallID: "native-call", AsyncQuestions: []api.AsyncQuestion{{Title: "选择", Options: []string{"甲", "乙"}}}}
	owner.states["work-one"] = api.WorkState{Task: api.Task{ID: "work-one", Title: "任务"}, AsyncQuestions: []api.Item{item}}
	a.observeWorkerQuestions(t.Context())
	item.AsyncQuestions[0].Options[1] = "丙"
	owner.states["work-one"] = api.WorkState{Task: api.Task{ID: "work-one", Title: "任务"}, AsyncQuestions: []api.Item{item}}
	a.observeWorkerQuestions(t.Context())
	if next := a.WorkerQuestions(); len(next) != 1 || next[0].ID != "Q2" || len(e.reports) != 2 || !strings.Contains(e.reports[1].Text, "Q2") {
		t.Fatalf("changed Worker question did not get its own report: %+v reports=%+v", next, e.reports)
	}
}
