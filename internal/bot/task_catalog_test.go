package bot

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type catalogFixture struct {
	api.TaskProvider
	query  api.TaskQuery
	pinned string
	calls  int
}

func (f *catalogFixture) QueryTasks(q api.TaskQuery) (api.TaskPage, error) {
	f.query = q
	f.calls++
	return api.TaskPage{Tasks: []api.TaskSummary{}, MaxRunning: 7, PinnedLimit: 8}, nil
}
func (f *catalogFixture) PinTask(id string, pin bool) (api.TaskSummary, error) {
	f.pinned = id
	f.calls++
	return api.TaskSummary{ID: id, Pinned: pin}, nil
}
func TestTaskToolRoutesBoundedQueriesAndPins(t *testing.T) {
	r, _, _ := fixture(t)
	f := &catalogFixture{}
	if e := r.ConfigureTasks(f, nil); e != nil {
		t.Fatal(e)
	}
	out := r.CallTool(t.Context(), "bot_tasks", json.RawMessage(`{"operation":"list","query":"review","pinned":false,"limit":10,"cursor":"cursor"}`))
	if out.IsError || f.query.Limit != 10 || f.query.Pinned == nil || *f.query.Pinned || f.query.Cursor != "cursor" {
		t.Fatal(out, f.query)
	}
	for _, op := range []string{"pin", "unpin"} {
		out = r.CallTool(t.Context(), "bot_tasks", json.RawMessage(`{"operation":"`+op+`","id":"owned"}`))
		if out.IsError || f.pinned != "owned" {
			t.Fatal(out)
		}
	}
	for _, bad := range []string{`{"operation":"delete"}`, `{"operation":"list","workspace":"elsewhere"}`, `{} {}`} {
		out = r.CallTool(t.Context(), "bot_tasks", json.RawMessage(bad))
		if !out.IsError {
			t.Fatal("unsupported mutation", bad)
		}
	}
	if f.calls != 3 {
		t.Fatal("bad arguments reached task manager")
	}
}
