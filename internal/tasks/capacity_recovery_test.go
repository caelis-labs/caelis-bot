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
