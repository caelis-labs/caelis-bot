package desktop

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

// A failed native read leaves desktop observations unknown. Never log native bytes, application
// names or error text; repeated sampling must not fill the diagnostic log.
type presenceSampleDecoder struct {
	mu    sync.Mutex
	at    time.Time
	write func(diagnosticlog.Record)
}

func (d *presenceSampleDecoder) decode(raw []byte) scheduledata.Sample {
	var v struct {
		Awake       bool
		Unlocked    *bool
		IdleSeconds float64
		Application string
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		reason := diagnosticlog.DecodeReason(err)
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.write != nil && (d.at.IsZero() || time.Since(d.at) >= 5*time.Minute) {
			d.at = time.Now()
			d.write(diagnosticlog.Record{Level: "warn", Component: "desktop", Code: "native_presence_invalid", Reason: reason})
		}
		return scheduledata.Sample{}
	}
	return scheduledata.Sample{Presence: scheduledata.Presence{Awake: v.Awake, Unlocked: v.Unlocked}, IdleSeconds: v.IdleSeconds, Application: v.Application}
}
