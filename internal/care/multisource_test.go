package care

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMultiSourceCanonicalIntentAndCoalescing(t *testing.T) {
	e := opened(t)
	r := rule("multi")
	r.On = ""
	r.OnAny = []string{"desktop.usage", "desktop.appChanged", "desktop.usage"}
	r.When = "has(event.activeSeconds) && event.activeSeconds >= 7200"
	grants := 0
	authorize := func(context.Context, string, string) error { grants++; return nil }
	first, err := e.Save(t.Context(), r, authorize)
	if err != nil {
		t.Fatal(err)
	}
	r.OnAny = []string{"desktop.appChanged", "desktop.usage"}
	again, err := e.Save(t.Context(), r, authorize)
	if err != nil || grants != 1 || first.Version != again.Version {
		t.Fatal("equivalent subscription changed grant", err)
	}
	for i, source := range r.OnAny {
		now := instant.Add(time.Duration(i) * time.Second)
		if err := e.Receive(t.Context(), Event{Source: source, At: now, Data: map[string]any{"activeSeconds": 7300}}, now); err != nil {
			t.Fatal(err)
		}
	}
	state := e.Snapshot()
	if len(state.Activations) != 1 || state.Activations[0].Source != "desktop.appChanged" || len(state.Watermarks) != 2 {
		t.Fatal(state)
	}
	state.Rules[0].OnAny[0] = "bad"
	reloaded, err := Open(e.path)
	if err != nil || !reflect.DeepEqual(reloaded.Snapshot().Rules, e.Snapshot().Rules) {
		t.Fatal("reload/isolation", err)
	}
}

func TestSingleSourceGrantEncodingUnchanged(t *testing.T) {
	e := opened(t)
	r := rule("single")
	first := save(t, e, r)
	raw, _ := json.Marshal(first.Rule)
	if strings.Contains(string(raw), "onAny") {
		t.Fatal("changed single-source grant bytes")
	}
	r.OnAny, r.On = []string{r.On, r.On}, ""
	again, err := e.Save(t.Context(), r, func(context.Context, string, string) error { t.Fatal("reauthorized identical legacy rule"); return nil })
	if err != nil || again.Version != first.Version || !reflect.DeepEqual(again.Rule, first.Rule) {
		t.Fatal(again, err)
	}
}

func TestMultiSourceUnavailableOnlyCancelsItsOwnPendingEvent(t *testing.T) {
	extra := Source{Name: "mail.changed", Description: "fixture"}
	e, err := OpenWithSources(filepath.Join(t.TempDir(), "state.json"), append(NativeSources(), extra))
	if err != nil {
		t.Fatal(err)
	}
	r := rule("multi")
	r.On = ""
	r.OnAny = []string{"desktop.usage", extra.Name}
	save(t, e, r)
	receive(t, e, instant)
	reloaded, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Snapshot().Rules[0].Issue != "source_unavailable" {
		t.Fatal("missing source not reported")
	}
	deliver(t, reloaded, instant)
	if reloaded.Snapshot().Activations[0].Status != "accepted" {
		t.Fatal("available source blocked")
	}
	for _, bad := range []Rule{
		{ID: "x", Label: "x", On: "clock.minute", OnAny: []string{"desktop.usage"}, Prompt: "x"},
		{ID: "x", Label: "x", OnAny: []string{"mail.unknown"}, Prompt: "x"},
	} {
		if _, err := e.Save(t.Context(), bad, nil); err == nil {
			t.Fatal("invalid sources accepted")
		}
	}
}
