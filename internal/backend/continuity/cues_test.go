package continuity

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func fixture(input int64) State {
	s := State{}
	s.Bind("resident", epoch.Add(-20*WindowWidth), epoch.Add(-10*WindowWidth))
	addDays(&s, epoch.Add(-3*WindowWidth), 4, input)
	return s
}
func addDays(s *State, start time.Time, days int, input int64) {
	for d := range days {
		for n := range 2 {
			at := start.Add(time.Duration(d) * WindowWidth).Add(time.Duration(n-1) * time.Hour)
			s.Observe(fmt.Sprint(at.Unix()), "model:included", input, at, at)
		}
	}
}
func TestCandidateContrasts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input int64
		want  bool
	}{
		{"stable low old session", 8_000, false}, {"high flat independent of window ratio", 64_000, true}, {"below absolute threshold", 63_999, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(tc.input)
			if s.Candidate(epoch) != tc.want {
				t.Fatal(s)
			}
		})
	}
	s := fixture(20_000)
	addDays(&s, epoch.Add(WindowWidth), 4, 40_000)
	if !s.Candidate(epoch.Add(4 * WindowWidth)) {
		t.Fatal("observed baseline growth was ignored")
	}
	s = fixture(1_000)
	addDays(&s, epoch.Add(WindowWidth), 4, 2_000)
	if s.Candidate(epoch.Add(4 * WindowWidth)) {
		t.Fatal("small doubling was treated as material")
	}
	s = fixture(64_000)
	s.Created = time.Time{} // Missing historical baseline/creation is not a permanent veto.
	if !s.Candidate(epoch) {
		t.Fatal("observed age lower bound did not qualify")
	}
	s.Since = epoch.Add(-MinAge + time.Second)
	if s.Candidate(epoch) {
		t.Fatal("too young")
	}
}
func TestSpikeDuplicatesAndUnknownAreNotPersistence(t *testing.T) {
	s := fixture(8_000)
	s.Observe("attachment", "model:included", 300_000, epoch, epoch)
	if s.Candidate(epoch) {
		t.Fatal("one spike triggered review")
	}
	for i := range 1000 {
		s.Observe("attachment", "model:included", 300_000, epoch.Add(time.Duration(i)*time.Second), epoch.Add(time.Duration(i)*time.Second))
	}
	if s.Candidate(epoch.Add(time.Hour)) {
		t.Fatal("same-turn steps supplied persistence")
	}
	s = State{}
	s.Bind("resident", epoch.Add(-MinAge), epoch)
	for d := range 4 {
		at := epoch.Add(time.Duration(d) * WindowWidth)
		s.Observe("one long turn", "model", 70_000, at, at)
	}
	if len(s.Windows) != 1 || s.Candidate(epoch.Add(3*WindowWidth)) {
		t.Fatal("one turn crossed windows as new votes")
	}
	s.Observe("zero", "model", 0, epoch, epoch)
	s.Observe("unknown", "", 70_000, epoch, epoch)
	if len(s.Windows) != 1 {
		t.Fatal("invalid observation counted")
	}
}
func TestPendingReceiptRestartAndFreshCooldownEvidence(t *testing.T) {
	s := fixture(70_000)
	text := s.Prepare("original", epoch)
	if text == "" {
		t.Fatal("missing cue")
	}
	s.Resolve("original", "unknown", epoch)
	b, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Prepare("other", epoch) != "" || restored.Prepare("original", epoch) != text {
		t.Fatal("uncertain delivery changed identity/text")
	}
	restored.Resolve("original", "accepted", epoch)
	if restored.Prepare("later", epoch.Add(time.Hour)) != "" {
		t.Fatal("cooldown ignored")
	}
	addDays(&restored, epoch.Add(WindowWidth), 4, 70_000)
	if restored.Candidate(epoch.Add(Cooldown)) {
		t.Fatal("evidence inside cooldown was reused")
	}
	addDays(&restored, epoch.Add(Cooldown+WindowWidth), 4, 70_000)
	if !restored.Candidate(epoch.Add(Cooldown + 4*WindowWidth)) {
		t.Fatal("fresh sustained evidence did not rearm")
	}
}
func TestBoundedRetentionConfigurationAndCompaction(t *testing.T) {
	s := fixture(70_000)
	for d := range 40 {
		for n := range 100 {
			at := epoch.Add(time.Duration(d)*WindowWidth + time.Duration(n)*time.Minute)
			s.Observe(fmt.Sprintf("%d/%d", d, n), "model:included", 70_000, at, at)
		}
	}
	if len(s.Windows) > int(Retention/WindowWidth) {
		t.Fatal("unbounded windows")
	}
	for _, w := range s.Windows {
		if len(w.Samples) > TurnsPerWindow {
			t.Fatal("unbounded turns")
		}
	}
	at := epoch.Add(40 * WindowWidth)
	s.Observe("new config", "different", 70_000, at, at)
	if len(s.Windows) != 1 || s.Baseline != 0 || s.Candidate(at) {
		t.Fatal("mixed configurations")
	}
	s.ForgetTurn("new config")
	if len(s.Windows) != 0 || !s.Latest.IsZero() {
		t.Fatal("compaction retained partial turn")
	}
}

func TestContinuitySparseBaselineAndDurableCounter(t *testing.T) {
	s := State{}
	s.Bind("resident", epoch.Add(-20*WindowWidth), epoch.Add(-20*WindowWidth))
	for d := range 8 {
		at := epoch.Add(time.Duration(d-7) * WindowWidth)
		s.Observe(fmt.Sprint(d), "model", 20_000, at, at, int64(d+1)*20_000)
	}
	if s.Baseline != 20_000 {
		t.Fatal("sparse ordinary turns never formed baseline", s.Baseline)
	}
	encoded, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	replay := epoch.Add(time.Hour)
	restored.Observe("7", "model", 90_000, replay, replay, 160_000)
	if !restored.Latest.Equal(epoch) || restored.LatestInput != 20_000 {
		t.Fatal("replay refreshed evidence after restart")
	}
	restored.Observe("7", "model", 18_000, replay, replay, 178_000)
	if restored.LatestInput != 18_000 || len(restored.Windows) != 8 {
		t.Fatal("fresh step did not keep one minimum vote")
	}
}
