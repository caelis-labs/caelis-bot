package continuity

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestInvitationRequiresWorkSpanAndFreshRecurringMaintenance(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	base := Observation{Since: now.Add(-14 * 24 * time.Hour), Turns: 64, CompletedAt: now}
	for i, turn := range []uint64{58, 61, 64} {
		base.Maintenance[i] = MaintenanceTurn{Turn: turn, At: now.Add(time.Duration(i-2) * time.Hour)}
	}
	if !Evaluate(base, 0, time.Time{}, now).Invites() {
		t.Fatal("recurring recent maintenance absent")
	}
	for _, scenario := range []string{"young", "little-work", "missing-age", "future-age", "settled", "old-maintenance", "one-turn-many-compacts", "consumed", "during-cooldown", "duplicate-turn", "future-observation", "legacy-aggregate"} {
		t.Run(scenario, func(t *testing.T) {
			o := base
			since := uint64(0)
			cutoff := time.Time{}
			switch scenario {
			case "young":
				o.Since = now.Add(-24 * time.Hour)
			case "little-work":
				since = 1
			case "missing-age":
				o.Since = time.Time{}
			case "future-age":
				o.Since = now.Add(time.Hour)
			case "settled":
				o.Turns++
			case "old-maintenance":
				o.Maintenance[0].Turn = 56
			case "one-turn-many-compacts":
				o.Compactions = 100
				o.Maintenance = [8]MaintenanceTurn{{Turn: 64, At: now}}
			case "consumed":
				since = 64
			case "during-cooldown":
				cutoff = now.Add(-time.Hour)
			case "duplicate-turn":
				o.Maintenance[1].Turn = 58
			case "future-observation":
				o.Maintenance[2].At = now.Add(time.Hour)
			case "legacy-aggregate":
				o.Compactions = 100
				o.Maintenance = [8]MaintenanceTurn{}
			}
			if Evaluate(o, since, cutoff, now).Invites() {
				t.Fatal("insufficient or invalid evidence invited", scenario)
			}
		})
	}
}

func TestInputTrendSpikeAndConfiguration(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var s State
	for i := range 8 {
		turn := fmt.Sprint(i)
		s.Start("resident", turn, now)
		input := int64(12000)
		if i >= 4 {
			input = 36000
		}
		s.Sample(turn, "same", input)
		s.Sample(turn, "same", input*10)
		s.Finish(turn, true, now)
	}
	s.Since = now.Add(-100 * 24 * time.Hour)
	s.Turns = 1000
	if Evaluate(s.Observation, 0, time.Time{}, now).Invites() {
		t.Fatal("request growth became semantic evidence")
	}
	s.Inputs = [8]int64{12000, 12000, 12000, 12000, 12000, 12000, 12000, 900000}
	if Evaluate(s.Observation, 0, time.Time{}, now).Invites() {
		t.Fatal("attachment became semantic evidence")
	}
	s.Start("resident", "new-config", now)
	s.Sample("new-config", "changed", 70000)
	s.Finish("new-config", true, now)
	if s.InputCount != 1 || s.Configuration != "changed" || s.Inputs[0] != 70000 {
		t.Fatal("mixed configurations", s)
	}
	// Failed or excluded maintenance turns cannot erase the business sample series.
	s.Start("resident", "failed", now)
	s.Sample("failed", "reviewer-model", 900000)
	s.Finish("failed", false, now)
	if s.InputCount != 1 || s.Configuration != "changed" {
		t.Fatal("failed turn reset business evidence", s)
	}
	s.Start("resident", "missing", now)
	s.Sample("missing", "changed", 0)
	s.Finish("missing", true, now)
	if s.InputKnown || s.InputCount != 1 {
		t.Fatal("missing became measured zero", s)
	}
}

func TestInvitationMaintenanceCountsTurnsAndSurvivesRestart(t *testing.T) {
	now := time.Now().Round(0)
	s := State{Observation: Observation{Session: "resident", Since: now.Add(-30 * 24 * time.Hour), Turns: 61}}
	for i := range 3 {
		turn := fmt.Sprint(i)
		s.Start("resident", turn, now)
		for j := range 3 {
			s.Compact(turn, fmt.Sprint(j))
		}
		s.Finish(turn, true, now)
	}
	data, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	e := Evaluate(restored.Observation, 0, time.Time{}, now)
	if !e.Invites() || e.RecentMaintenance != 3 || restored.Compactions != 9 {
		t.Fatal(e, restored)
	}
	restored.Start("resident", "settled", now)
	restored.Finish("settled", true, now)
	if Evaluate(restored.Observation, 0, time.Time{}, now).Invites() {
		t.Fatal("settled completion retained candidate")
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
		s.Start("resident", "next", now)
		s.Sample("next", "model", 12_000)
		if !s.Finish("next", true, now) || s.InputCount != 1 || !s.InputKnown || s.Inputs[0] != 12_000 {
			t.Fatal("invalid persisted count prevented fresh observation", s)
		}
	}
}

func TestStableHighInputIsNotSemanticHealthEvidence(t *testing.T) {
	now := time.Now()
	for _, input := range []int64{500, 64000, 256000, 900000} {
		o := Observation{Since: now.Add(-100 * 24 * time.Hour), Turns: 1000, InputKnown: true, InputCount: 8}
		for i := range o.Inputs {
			o.Inputs[i] = input
		}
		if Evaluate(o, 0, time.Time{}, now).Invites() {
			t.Fatal("stable useful context invited solely by input scale", input)
		}
	}
}
