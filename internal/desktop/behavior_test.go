package desktop

import (
	"math"
	"testing"
)

type propDriver struct {
	fakeDriver
	launches int
	ready    bool
	finished string
}

func (d *propDriver) planeReady(v bool) { d.ready = v }
func (d *propDriver) launchPlane(id string, x, y float64) bool {
	if !d.ready {
		return false
	}
	d.launches++
	return true
}
func (d *propDriver) finishPlane(id string, completed bool) { d.finished = id }
func TestPropAuthorityAndLifecycle(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &propDriver{fakeDriver: fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}}
	s.start(d)
	if ok, _ := s.LaunchPlane("first", 90, 100); ok {
		t.Fatal("unready renderer accepted prop")
	}
	s.PlaneReady(true)
	if ok, err := s.LaunchPlane("first", 90, 100); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, x := range []float64{math.NaN(), math.Inf(1), -1, 181} {
		if _, err := s.LaunchPlane("bad", x, 100); err == nil {
			t.Fatal("invalid release accepted")
		}
	}
	for _, state := range []string{"working", "waiting"} {
		s.characterActivity = state
		if ok, _ := s.LaunchPlane("busy", 90, 100); ok {
			t.Fatal("active work accepted idle prop")
		}
	}
	s.characterActivity = "idle"
	if err := s.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.LaunchPlane("hidden", 90, 100); ok {
		t.Fatal("hidden prop accepted")
	}
	s.shutdown()
	s.PlaneReady(true)
	s.FinishPlane("late", true)
	if d.finished != "" || d.launches != 1 {
		t.Fatal("late callback reached stopped native host")
	}
}
