package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"testing"
)

func TestWindowsDockUsesFeetAnchorAndBoundedFan(t *testing.T) {
	area := application.Rect{X: -1920, Y: 0, Width: 1920, Height: 1040}
	pet := application.Rect{X: -1000, Y: 250, Width: 180, Height: 240}
	compact := windowsTaskPlacement(pet, area, 3, false)
	if compact.Width != 62 || compact.Height != 28 || compact.Y != 454 {
		t.Fatal(compact)
	}
	fan := windowsTaskPlacement(pet, area, 3, true)
	if fan.Vertical || fan.Width != 568 || fan.Height != 196 || fan.Y != 454 {
		t.Fatal(fan)
	}
	pet.X, pet.Y = -190, 780
	side := windowsTaskPlacement(pet, area, 3, true)
	if !side.Vertical || !side.Reverse || side.Width != 240 || side.X >= pet.X {
		t.Fatal(side)
	}
	for _, count := range []int{1, 2, 30} {
		v := windowsTaskPlacement(pet, area, count, true)
		if !v.Vertical || v.Y < pet.Y || v.X+v.Width > pet.X {
			t.Fatalf("task cards invaded bubble space: count=%d layout=%+v", count, v)
		}
	}
	for _, count := range []int{0, 1, 3, 10, 500} {
		for _, size := range []application.Rect{area, {Width: 320, Height: 240}, {X: 1920, Y: -200, Width: 1280, Height: 720}} {
			for _, point := range []application.Rect{{X: size.X, Y: size.Y, Width: 180, Height: 240}, {X: size.X + size.Width - 180, Y: size.Y + size.Height - 240, Width: 180, Height: 240}} {
				v := windowsTaskPlacement(point, size, count, true)
				if v.X < size.X+8 || v.Y < size.Y+8 || v.X+v.Width > size.X+size.Width-8 || v.Y+v.Height > size.Y+size.Height-8 {
					t.Fatal(count, size, v)
				}
			}
		}
	}
}
