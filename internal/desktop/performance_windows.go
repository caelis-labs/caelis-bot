package desktop

import (
	"fmt"
	"math"
	"time"
)

// Durations only: never collect model names, file paths, image bytes or prompts.
func (s *Service) WindowsUIPerformance(phase string, milliseconds float64) {
	switch phase {
	case "SetupProfile", "InspectSetup", "SetupModels", "SetupOverview", "ExecutionSettings", "LocalWorkerSelection":
	default:
		return
	}
	if math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds < 0 || milliseconds > 120000 {
		return
	}
	s.mu.Lock()
	d, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if ok && d.diagnostic != nil {
		d.diagnostic("settings_timing", fmt.Sprintf("%s %.1fms", phase, milliseconds))
	}
}

func (c *windowsCapture) timing(phase string, start time.Time) {
	if !start.IsZero() && c.driver.diagnostic != nil {
		c.driver.diagnostic("capture_timing", fmt.Sprintf("%s %.1fms", phase, float64(time.Since(start).Microseconds())/1000))
	}
}
