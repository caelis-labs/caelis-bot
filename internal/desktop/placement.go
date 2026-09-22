package desktop

import "math"

const (
	BaseWidth  = 180.0
	BaseHeight = 240.0
	MinScale   = 0.65
	MaxScale   = 1.6
)

// Rect is the host contract: logical units, +Y up, global origin at the primary
// display's bottom left. Drivers convert OS coordinates/DPI at the boundary;
// physical render pixels never enter persisted geometry. See platform-baseline.md.
type Rect struct{ X, Y, Width, Height float64 }
type Placement struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Scale      float64 `json:"scale"`
	Visible    bool    `json:"visible"`
	Positioned bool    `json:"positioned"`
}

func defaults() Placement   { return Placement{Scale: 1, Visible: true} }
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func normalize(p Placement, screens []Rect) Placement {
	if !finite(p.Scale) || p.Scale <= 0 {
		p.Scale = 1
	}
	p.Scale = math.Max(MinScale, math.Min(MaxScale, p.Scale))
	if len(screens) == 0 {
		return p
	}
	if !finite(p.X) || !finite(p.Y) {
		p.Positioned = false
	}
	w, h := BaseWidth*p.Scale, BaseHeight*p.Scale
	best, area := screens[0], -1.0
	for _, s := range screens {
		a := math.Max(0, math.Min(p.X+w, s.X+s.Width)-math.Max(p.X, s.X)) * math.Max(0, math.Min(p.Y+h, s.Y+s.Height)-math.Max(p.Y, s.Y))
		if a > area {
			best, area = s, a
		}
	}
	if !p.Positioned || area == 0 {
		p.X, p.Y = best.X+best.Width-w-28, best.Y+28
	}
	p.X = math.Max(best.X, math.Min(p.X, best.X+best.Width-w))
	p.Y = math.Max(best.Y, math.Min(p.Y, best.Y+best.Height-h))
	p.Positioned = true
	return p
}
