// Package continuity holds bounded observations for optional memory review.
// It does not measure stale content, cost, or authorize session replacement.
package continuity

import "time"

// Observer is a read-only adapter boundary. Only live resident turn completion
// advances the observation; history and reconnect cannot manufacture activity.
type Observer interface{ ContinuityObservation() Observation }

type MaintenanceTurn struct {
	Turn uint64    `json:"turn"`
	At   time.Time `json:"at"`
}

type Observation struct {
	Maintenance [8]MaintenanceTurn `json:"maintenance"` // one per completed business turn

	Session       string    `json:"session"`
	Since         time.Time `json:"since"` // first live observation, an age lower bound
	Turn          string    `json:"turn"`
	CompletedAt   time.Time `json:"completedAt"`
	Turns         uint64    `json:"turns"`
	Compactions   uint64    `json:"compactions"` // observed successful compactions only
	Configuration string    `json:"configuration,omitempty"`
	Inputs        [8]int64  `json:"inputs"` // positive per-turn minima, oldest first
	InputCount    int       `json:"inputCount"`
	InputKnown    bool      `json:"inputKnown"` // latest completed turn, never missing-as-zero
}

type State struct {
	Observation
	Active              string `json:"-"`
	ActiveConfiguration string `json:"-"`
	Input               int64  `json:"-"`
	Compacted           bool   `json:"-"`
	CompactID           string `json:"-"`
	CompactCount        uint64 `json:"-"`
	Total               int64  `json:"-"` // native usage deduplication within this live turn
}

func (s *State) Start(session, turn string, now time.Time) {
	if session == "" || turn == "" || now.IsZero() {
		return
	}
	if s.Session != session {
		*s = State{Observation: Observation{Session: session, Since: now}}
	}
	if s.Active == turn || s.Turn == turn {
		return
	}
	s.Active, s.ActiveConfiguration, s.Input, s.Compacted, s.CompactID, s.Total, s.CompactCount = turn, "", 0, false, "", 0, 0
}

func (s *State) Sample(turn, configuration string, input int64) {
	if s.Active != turn || turn == "" || configuration == "" || input <= 0 || s.Compacted {
		return
	}
	if s.ActiveConfiguration != configuration {
		s.ActiveConfiguration, s.Input = configuration, 0
	}
	if s.Input == 0 || input < s.Input {
		s.Input = input
	}
}

func (s *State) Compact(turn, id string) {
	if turn == "" || s.Active != turn || id == "" || s.CompactID == id {
		return
	}
	s.CompactID, s.Compacted, s.Input = id, true, 0
	s.CompactCount++
}

func (s *State) Finish(turn string, completed bool, now time.Time) bool {
	if turn == "" || s.Active != turn {
		return false
	}
	s.Active = ""
	if !completed || now.IsZero() || now.Before(s.CompletedAt) {
		return false
	}
	s.Turn, s.CompletedAt, s.Turns = turn, now, s.Turns+1
	s.Compactions += s.CompactCount
	if s.Compacted {
		copy(s.Maintenance[:], s.Maintenance[1:])
		s.Maintenance[len(s.Maintenance)-1] = MaintenanceTurn{Turn: s.Turns, At: now}
	}
	if s.InputCount < 0 || s.InputCount > len(s.Inputs) || s.ActiveConfiguration != "" && s.Configuration != s.ActiveConfiguration {
		s.Configuration, s.Inputs, s.InputCount = s.ActiveConfiguration, [8]int64{}, 0
	}
	s.InputKnown = s.Input > 0 && !s.Compacted
	if s.InputKnown {
		if s.InputCount == len(s.Inputs) {
			copy(s.Inputs[:], s.Inputs[1:])
			s.InputCount--
		}
		s.Inputs[s.InputCount] = s.Input
		s.InputCount++
	}
	return true
}
