package desktop

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type characterDriver struct {
	fakeDriver
	states []string
}

func TestAnimatedPackedMaskKeepsPixelOrder(t *testing.T) {
	s, d, _ := setup()
	packed := make([]byte, int(BaseWidth*BaseHeight)/8)
	want := map[int]bool{0: true, 7: true, 8: true, 179: true, 180: true, 43199: true}
	for i := range want {
		packed[i/8] |= 1 << (i % 8)
	}
	if err := s.SetPackedHitMask(context.Background(), base64.StdEncoding.EncodeToString(packed)); err != nil {
		t.Fatal(err)
	}
	for i, pixel := range d.hit {
		if (pixel == 1) != want[i] {
			t.Fatalf("pixel %d changed during packed transport", i)
		}
	}
	if s.SetPackedHitMask(context.Background(), "bad") == nil {
		t.Fatal("invalid packed mask accepted")
	}
}

func (d *characterDriver) activity(value string) { d.states = append(d.states, value) }
func TestCharacterConsumesFactsWithoutRepeatingAttention(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &characterDriver{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}
	s.start(d)
	s.observeCharacter(api.Snapshot{CanInterrupt: true})
	v := api.Snapshot{CanInterrupt: true, Approvals: []api.Approval{{Status: "pending"}}}
	s.observeCharacter(v)
	s.observeCharacter(v)
	if s.CharacterActivity() != "waiting" || len(d.states) != 2 {
		t.Fatal("pending approval did not override working exactly once")
	}
	v.Approvals[0].Status = "resolved"
	s.observeCharacter(v)
	s.observeCharacter(api.Snapshot{Phase: "completed"})
	if s.CharacterActivity() != "idle" || d.panelOpen || d.stopped {
		t.Fatal("character facts changed native lifecycle")
	}
	s.shutdown()
	s.observeCharacter(v)
	if len(d.states) != 4 {
		t.Fatal("character updated after shutdown")
	}
}

func TestDreamCharacterYieldsToApprovalAndRecovery(t *testing.T) {
	v := api.Snapshot{Maintenance: "dreaming", Connection: "ready", Phase: "working", CanInterrupt: true}
	if characterActivity(v) != "dreaming" {
		t.Fatal("missing nap")
	}
	v.Approvals = []api.Approval{{Status: "pending"}}
	if characterActivity(v) != "waiting" {
		t.Fatal("nap masked approval")
	}
	v.Approvals = nil
	for _, phase := range []string{"completed", "interrupting", "unknown", "failed"} {
		v.Phase = phase
		if characterActivity(v) == "dreaming" {
			t.Fatal("stale nap", phase)
		}
	}
}
