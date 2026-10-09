package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type retiringRuntime struct{ *fixtureRuntime }

type unreadableRuntime struct {
	*retiringRuntime
	unreadable map[string]bool
	selected   string
	owners     map[string]string
}

func newUnreadableRuntime() *unreadableRuntime {
	return &unreadableRuntime{retiringRuntime: &retiringRuntime{newRuntime()}, unreadable: map[string]bool{}, owners: map[string]string{}}
}

func (f *unreadableRuntime) ReadWork(_ context.Context, id string) (api.Task, error) {
	if f.unreadable[id] {
		return api.Task{}, errors.New("original owner unavailable")
	}
	s, ok := f.states[id]
	if !ok {
		return api.Task{}, errors.New("missing original owner")
	}
	if s.Task.Status == "unknown" && s.Activity == "idle" {
		s.Task.Status = "unavailable"
		f.states[id] = s
	}
	return s.Task, nil
}
func (f *unreadableRuntime) RetireWork(ctx context.Context, id string) (api.Task, error) {
	if f.unreadable[id] {
		return api.Task{}, errors.New("original owner unavailable")
	}
	return f.retiringRuntime.RetireWork(ctx, id)
}
func (f *unreadableRuntime) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	s := f.states[in.ID]
	if s.Task.Status == "unknown" || s.Task.Status == "unavailable" {
		return s.Task, errors.New("original receipt unresolved or retired")
	}
	return f.fixtureRuntime.SendWork(ctx, in)
}

func (f *unreadableRuntime) BindWork(_ context.Context, _ api.TaskStart, id, original string) (string, error) {
	if original != "" {
		return original, nil
	}
	f.owners[id] = f.selected
	return f.selected, nil
}
func (f *unreadableRuntime) OwnsWork(runtime string) bool {
	return runtime == "codex" || runtime == "caelis"
}
func (f *unreadableRuntime) WorkStates() []api.WorkState {
	states := f.fixtureRuntime.WorkStates()
	for i := range states {
		states[i].Runtime = f.owners[states[i].Task.ID]
	}
	return states
}

func (f *retiringRuntime) RetireWork(_ context.Context, id string) (api.Task, error) {
	s, ok := f.states[id]
	if !ok {
		return api.Task{}, errors.New("missing original owner")
	}
	if s.Activity != "idle" {
		return s.Task, errors.New("original owner is active or unconfirmed")
	}
	s.Task.Status = "unavailable"
	f.states[id] = s
	return s.Task, nil
}

func TestWorkCapacitySeparatesHistoryUnknownAndWatchlist(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			f := &retiringRuntime{newRuntime()}
			root := t.TempDir()
			m, err := Open(root+"/tasks.json", root+"/Tasks", provider, f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			m.ConfigureLimit(func() int { return 2 })
			first := start(t, m, "capacity-first")
			second := start(t, m, "capacity-second")
			if _, err := m.StartTask(t.Context(), input("capacity-third")); err == nil {
				t.Fatal("parallel work exceeded limit")
			}
			if _, err := m.PinTask(first.ID, false); err != nil {
				t.Fatal(err)
			}
			if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 2 {
				t.Fatal("unpin changed work concurrency", page, err)
			}
			s := f.states[first.ID]
			s.Task.Status = "completed"
			f.states[first.ID] = s
			s = f.states[second.ID]
			s.Task.Status, s.Activity = "unknown", "active"
			f.states[second.ID] = s
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 {
				t.Fatal("terminal or active unknown count wrong", page, err)
			}
			third := start(t, m, "capacity-third")
			if _, err := m.StartTask(t.Context(), input("capacity-fourth")); err == nil {
				t.Fatal("active unknown escaped limit")
			}
			s = f.states[second.ID]
			s.Activity = ""
			f.states[second.ID] = s
			if _, err := m.StartTask(t.Context(), input("capacity-fourth")); err == nil {
				t.Fatal("unconfirmed activity escaped admission")
			}
			if _, err := m.RetireTask(t.Context(), second.ID); err == nil {
				t.Fatal("retired without native idle proof")
			}
			s.Activity = "idle"
			f.states[second.ID] = s
			if v, err := m.RetireTask(t.Context(), second.ID); err != nil || v.Status != "unavailable" {
				t.Fatal(v, err)
			}
			if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: second.ID, RequestID: "new-after-retire", Prompt: "resume"}); err == nil {
				t.Fatal("retired task resumed")
			}
			if v, err := m.ReadTask(t.Context(), second.ID); err != nil || v.Status != "unavailable" {
				t.Fatal("retired history lost", v, err)
			}
			if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 {
				t.Fatal("retired slot not released", page, err)
			}
			_ = third
			start(t, m, "capacity-fourth")
			m, err = Open(root+"/tasks.json", root+"/Tasks", provider, f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if v, err := m.ReadTask(t.Context(), second.ID); err != nil || v.Status != "unavailable" {
				t.Fatal("retired task revived after restart", v, err)
			}
		})
	}
}

func TestParallelStartAdmissionReservesOneSlot(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "codex", f)
	m.ConfigureLimit(func() int { return 1 })
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() { _, err := m.StartTask(t.Context(), input(fmt.Sprintf("parallel-%02d", i))); results <- err })
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 || f.starts != 1 {
		t.Fatal("admission raced", accepted, f.starts)
	}
}

func TestOneUnreadableUnknownReservesOnlyItsOwnSlot(t *testing.T) {
	f := newUnreadableRuntime()
	f.selected = "codex"
	root := t.TempDir()
	m, err := Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	old := start(t, m, "original-unknown")
	s := f.states[old.ID]
	s.Task.Status, s.Task.Outcome, s.Activity = "unknown", "rejected", ""
	f.states[old.ID] = s
	f.unreadable[old.ID] = true
	m, err = Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	f.selected = "caelis"
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 || page.Reserved != 1 || page.MaxRunning != 8 {
		t.Fatal("uncertain capacity was not shown separately", page, err)
	}
	for i := range 7 {
		v := start(t, m, fmt.Sprintf("healthy-caelis-%d", i))
		if f.owners[v.ID] != "caelis" {
			t.Fatal("healthy task routed away from Caelis", v, f.owners[v.ID])
		}
	}
	if f.starts != 8 || f.owners[old.ID] != "codex" {
		t.Fatal("unreadable Codex owner blocked independent Caelis starts", f.starts, f.owners)
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 7 || page.Reserved != 1 {
		t.Fatal("full capacity was misreported", page, err)
	}
	if _, err := m.StartTask(t.Context(), input("ninth-potential-worker")); err == nil || f.starts != 8 {
		t.Fatal("unreadable unknown escaped its reserved slot", err, f.starts)
	}
	if _, err := m.RetireTask(t.Context(), old.ID); err == nil {
		t.Fatal("unreadable original was retired")
	}
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: old.ID, RequestID: "new-continuation", Prompt: "resume"}); err == nil {
		t.Fatal("unknown original was continued")
	}
	f.unreadable[old.ID] = false
	s = f.states[old.ID]
	s.Activity = "idle" // Only a positive original-owner read may release it.
	f.states[old.ID] = s
	start(t, m, "ninth-potential-worker")
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 8 || page.Reserved != 0 {
		t.Fatal("idle original did not release its slot", page, err)
	}
	m, err = Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if task, err := m.ReadTask(t.Context(), old.ID); err != nil || task.Status != "unavailable" || task.Outcome != "rejected" {
		t.Fatal("retired original was lost on restart", task, err)
	}
}

func TestMultipleUnreadableUnknownsUseOneReservationEach(t *testing.T) {
	f := newUnreadableRuntime()
	f.selected = "codex"
	m, err := Open(t.TempDir()+"/tasks.json", t.TempDir()+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	var ids []string
	for i := range 8 {
		v := start(t, m, fmt.Sprintf("old-unknown-%d", i))
		ids = append(ids, v.ID)
	}
	for _, id := range ids {
		s := f.states[id]
		s.Task.Status, s.Activity = "unknown", ""
		f.states[id] = s
		f.unreadable[id] = true
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 || page.Reserved != 8 {
		t.Fatal("unknown reservation count wrong", page, err)
	}
	if _, err := m.StartTask(t.Context(), input("beyond-eight")); err == nil || f.starts != 8 {
		t.Fatal("eight possible active owners admitted another start", err, f.starts)
	}
	delete(f.unreadable, ids[0])
	s := f.states[ids[0]]
	s.Activity = "idle"
	f.states[ids[0]] = s
	start(t, m, "beyond-eight")
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 || page.Reserved != 7 {
		t.Fatal("one confirmed idle owner did not release exactly one slot", page, err)
	}
}

func TestUnreadableUnknownDoesNotBlockIndependentTerminalRestart(t *testing.T) {
	f := newUnreadableRuntime()
	f.selected = "codex"
	m, err := Open(t.TempDir()+"/tasks.json", t.TempDir()+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	old := start(t, m, "unknown-original")
	s := f.states[old.ID]
	s.Task.Status, s.Activity = "unknown", ""
	f.states[old.ID] = s
	f.unreadable[old.ID] = true
	other := start(t, m, "healthy-original")
	f.complete(other.ID)
	continued, err := m.SendTask(t.Context(), api.TaskMessage{ID: other.ID, RequestID: "healthy-continuation", Prompt: "continue the completed task"})
	if err != nil || continued.Status != "working" || f.states[old.ID].Task.Status != "unknown" {
		t.Fatal("unreadable unrelated original blocked a healthy continuation", continued, err)
	}
}

func TestParallelAdmissionWithUncertainReservation(t *testing.T) {
	f := newUnreadableRuntime()
	f.selected = "codex"
	m, err := Open(t.TempDir()+"/tasks.json", t.TempDir()+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	old := start(t, m, "old-unreadable")
	s := f.states[old.ID]
	s.Task.Status, s.Activity = "unknown", ""
	f.states[old.ID] = s
	f.unreadable[old.ID] = true
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := range 20 {
		wg.Go(func() {
			_, err := m.StartTask(t.Context(), input(fmt.Sprintf("parallel-with-unknown-%02d", i)))
			results <- err
		})
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 7 || f.starts != 8 {
		t.Fatal("parallel starts claimed reserved slot", accepted, f.starts)
	}
}
