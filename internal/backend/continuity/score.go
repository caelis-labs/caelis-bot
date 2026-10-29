package continuity

import (
	"slices"
	"time"
)

const Cooldown = 7 * 24 * time.Hour
const Threshold = 3.0

// Score is additive evidence for inviting a review, not a probability or a
// health verdict. Age and turns together cannot reach Threshold.
type Score struct{ Age, Turns, Compactions, Input float64 }

func (s Score) Total() float64 { return s.Age + s.Turns + s.Compactions + s.Input }

func Confidence(o Observation, sinceTurns, sinceCompactions uint64, now time.Time) Score {
	s := Score{}
	if !o.Since.IsZero() {
		s.Age = min(1, max(0, now.Sub(o.Since).Hours()/(14*24)))
	}
	if o.Turns >= sinceTurns {
		s.Turns = min(1, float64(o.Turns-sinceTurns)/64)
	}
	if o.Compactions >= sinceCompactions {
		s.Compactions = min(2, float64(o.Compactions-sinceCompactions)/3)
	}
	if !o.InputKnown || o.InputCount < 1 || o.InputCount > len(o.Inputs) {
		return s
	}
	values := o.Inputs[:o.InputCount]
	// Below 32k supplies no elevated-input evidence; 64k supplies one point.
	// Eight independent turn minima and a lower median damp a single large input.
	level := median(values)
	strength := min(1, max(0, float64(level-32_000)/32_000))
	if len(values) == len(o.Inputs) {
		earlier, recent := median(values[:4]), median(values[4:])
		if earlier > 0 && recent > earlier {
			growth := min(float64(recent-earlier)/float64(earlier), float64(recent-earlier)/16_000)
			strength = max(strength, min(1, growth))
		}
	}
	s.Input = strength * float64(len(values)) / float64(len(o.Inputs))
	return s
}
func median(values []int64) int64 {
	copy := slices.Clone(values)
	slices.Sort(copy)
	return copy[(len(copy)-1)/2]
}
