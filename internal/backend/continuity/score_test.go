package continuity

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestConfidenceTimeAccumulationAndMissingSignals(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	base := Observation{Since: now.Add(-30 * 24 * time.Hour), Turns: 64, InputKnown: true, InputCount: 8}
	for i := range base.Inputs {
		base.Inputs[i] = 20_000
	}
	if score := Confidence(base, 0, 0, now); score.Total() != 2 {
		t.Fatal("healthy old context invited", score)
	}
	for i := range base.Inputs {
		base.Inputs[i] = 70_000
	}
	if score := Confidence(base, 0, 0, now); score.Total() != Threshold {
		t.Fatal("sustained high observations did not accumulate", score)
	}
	base.Since = now
	if Confidence(base, 0, 0, now.Add(7*24*time.Hour)).Total() != 2.5 {
		t.Fatal("time credit not gradual")
	}
	if Confidence(base, 0, 0, now.Add(14*24*time.Hour)).Total() != Threshold {
		t.Fatal("elapsed time missing")
	}
	if Confidence(base, 0, 0, now.Add(-time.Hour)).Age != 0 {
		t.Fatal("clock rollback added age")
	}
	base.Since = now.Add(-30 * 24 * time.Hour)
	base.Turns = 63
	if Confidence(base, 0, 0, now).Total() >= Threshold {
		t.Fatal("turn credit rounded upward")
	}
	base.Turns, base.InputKnown, base.Compactions = 64, false, 3
	if score := Confidence(base, 0, 0, now); score.Total() != Threshold || score.Input != 0 {
		t.Fatal("missing input blocked other evidence", score)
	}
	if score := Confidence(base, 64, 3, now); score.Total() != 1 {
		t.Fatal("old activity reused after invitation", score)
	}
}

func TestInputTrendSpikeAndConfiguration(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var s State
	for i := range 8 {
		turn := fmt.Sprint(i)
		s.Start("resident", turn, now)
		input := int64(12_000)
		if i >= 4 {
			input = 36_000
		}
		s.Sample(turn, "same", input)
		s.Sample(turn, "same", input*10) // multi-step turn has only one minimum vote
		s.Finish(turn, true, now)
	}
	if got := Confidence(s.Observation, 0, 0, now).Input; got != 1 {
		t.Fatal("relative input rise missing", got)
	}
	s.Inputs = [8]int64{12000, 12000, 12000, 12000, 12000, 12000, 12000, 900000}
	if Confidence(s.Observation, 0, 0, now).Input != 0 {
		t.Fatal("one attachment became sustained evidence")
	}
	s.Start("resident", "new-config", now)
	s.Sample("new-config", "changed", 70_000)
	s.Finish("new-config", true, now)
	if s.InputCount != 1 || Confidence(s.Observation, 0, 0, now).Input != .125 {
		t.Fatal("mixed configurations", s)
	}
	s.Start("resident", "missing", now)
	s.Sample("missing", "changed", 0)
	s.Finish("missing", true, now)
	if s.InputKnown || s.InputCount != 1 || Confidence(s.Observation, 0, 0, now).Input != 0 {
		t.Fatal("missing was treated as measured zero or old data", s)
	}
}

func TestObservationCompletionReplayAndRestart(t *testing.T) {
	now := time.Now().Round(0)
	var s State
	s.Start("one", "turn", now)
	s.Sample("turn", "model", 70_000)
	s.Compact("turn", "compact")
	s.Compact("turn", "compact")
	if !s.Finish("turn", true, now) || s.InputKnown || s.Compactions != 1 || s.Turns != 1 {
		t.Fatal(s)
	}
	s.Start("one", "turn", now)
	if s.Finish("turn", true, now) {
		t.Fatal("completion replay counted")
	}
	s.Start("one", "in-progress", now)
	s.Sample("in-progress", "model", 80_000)
	data, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Finish("in-progress", true, now) || restored.Turns != 1 {
		t.Fatal("restart invented live observations")
	}
	restored.Start("two", "fresh", now.Add(time.Hour))
	if restored.Turns != 0 || restored.Compactions != 0 || !restored.Since.Equal(now.Add(time.Hour)) {
		t.Fatal("new binding retained old evidence")
	}
}

func TestObservationInvalidPersistedSampleCount(t *testing.T) {
	for _, count := range []int{-1, 9} {
		now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
		s := State{Observation: Observation{Session: "resident", Configuration: "model", InputCount: count, InputKnown: true}}
		if Confidence(s.Observation, 0, 0, now).Input != 0 {
			t.Fatal("invalid series supplied evidence")
		}
		s.Start("resident", "next", now)
		s.Sample("next", "model", 12_000)
		if !s.Finish("next", true, now) || s.InputCount != 1 || !s.InputKnown || s.Inputs[0] != 12_000 {
			t.Fatal("invalid persisted count prevented fresh observation", s)
		}
	}
}
