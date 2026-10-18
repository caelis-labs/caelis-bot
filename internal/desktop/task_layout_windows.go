package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// Windows uses top-left DIP coordinates. Keep the Mac dock's bounded fan,
// feet anchor and side-stack behavior without importing AppKit geometry.
type WindowsTaskLayout struct {
	X        int  `json:"-"`
	Y        int  `json:"-"`
	Width    int  `json:"width"`
	Height   int  `json:"height"`
	Vertical bool `json:"vertical"`
	Reverse  bool `json:"reverse"`
}

func windowsTaskPlacement(pet, area application.Rect, count int, expanded bool) WindowsTaskLayout {
	if area.Width <= 16 || area.Height <= 16 {
		area = application.Rect{Width: 800, Height: 600}
	}
	left, top, right, bottom := area.X+8, area.Y+8, area.X+area.Width-8, area.Y+area.Height-8
	clamp := func(v, lo, hi int) int { return max(lo, min(v, max(lo, hi))) }
	feet := pet.Y + pet.Height - 44*pet.Height/240
	anchor := clamp(feet, top, bottom)
	if !expanded {
		return WindowsTaskLayout{X: clamp(pet.X+pet.Width/2-31, left, right-62), Y: clamp(feet+8, top, bottom-28), Width: min(62, right-left), Height: min(28, bottom-top)}
	}
	maxWidth := min(right-left, max(256, min(760, (right-left)*65/100)))
	maxHeight := min(bottom-top, max(196, min(620, (bottom-top)*70/100)))
	width, height := min(maxWidth, 256+max(0, count-1)*156), min(196, bottom-top)
	below := max(0, bottom-anchor-8)
	centered := pet.X+pet.Width/2-width/2 >= left && pet.X+pet.Width/2+width/2 <= right
	rightSpace, leftSpace := right-pet.X-pet.Width-8, pet.X-8-left
	// Reserve everything above the pet for its bubble. A single card needs the
	// same side placement as a stack when there is no room beneath the feet.
	sideTop := clamp(pet.Y, top, bottom-1)
	up, down := max(0, anchor-sideTop), max(0, bottom-anchor)
	if (!centered || below < height) && max(leftSpace, rightSpace) > 0 {
		reverse := up >= down
		space := down
		if reverse {
			space = up
		}
		height = max(1, min(maxHeight, 196+max(0, count-1)*116, space))
		width = min(240, max(leftSpace, rightSpace))
		x, y := pet.X+pet.Width+8, anchor
		if leftSpace > rightSpace {
			x = pet.X - 8 - width
		}
		if reverse {
			y -= height
		}
		return WindowsTaskLayout{X: clamp(x, left, right-width), Y: clamp(y, sideTop, bottom-height), Width: width, Height: height, Vertical: true, Reverse: reverse}
	}
	y := feet + 8
	// Very narrow displays can have no side space. Shrink within the area at
	// or below the pet instead of flipping into the bubble's reading space.
	height = min(height, bottom-sideTop)
	return WindowsTaskLayout{X: clamp(pet.X+pet.Width/2-width/2, left, right-width), Y: clamp(y, top, bottom-height), Width: width, Height: height}
}

func (s *Service) WindowsTaskDockLayout() WindowsTaskLayout {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(*windowsDriver); ok && !s.stopped {
		if value := d.taskLayout.Load(); value != nil {
			return value.(WindowsTaskLayout)
		}
	}
	return WindowsTaskLayout{Width: 256, Height: 196}
}
