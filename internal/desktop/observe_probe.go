package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

// Explicit development CLI only. Same observer as the resident tool, without
// opening a model session. Captured bytes stay in the isolated private profile.
func (s *Service) runObservationProbe(root string) {
	if os.Getenv("CAELIS_BOT_DATA_DIR") == "" || s.desktopExperiment() == nil {
		log.Print("DESKTOP OBSERVE FAIL: isolated profile and experiment flag required")
		return
	}
	time.Sleep(2 * time.Second) // Let the first renderer frame arrive before the one-shot capture.
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	frame, err := s.desktopExperiment()(ctx)
	if err == nil {
		_, err = desktopcontrol.Result(frame)
	}
	if err == nil {
		var raw []byte
		raw, err = json.Marshal(frame)
		if err == nil {
			err = os.WriteFile(filepath.Join(root, "desktop-observation.json"), raw, 0600)
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(root, "desktop-observation.jpg"), frame.Image, 0600)
	}
	if err != nil {
		log.Print("DESKTOP OBSERVE FAIL: ", err)
		return
	}
	if !frame.World.ActorVisible {
		log.Print("DESKTOP OBSERVE FAIL: ", errors.New("actor is hidden"))
		return
	}
	log.Print("DESKTOP OBSERVE PASS: real capture and matching geometry; model grounding not tested")
}
