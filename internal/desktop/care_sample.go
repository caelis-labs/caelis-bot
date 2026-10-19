package desktop

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// A failed native read degrades care alone. Never log native bytes, application
// names or error text; repeated sampling must not fill the diagnostic log.
type careSampleDecoder struct {
	mu    sync.Mutex
	at    time.Time
	write func(diagnosticlog.Record)
}

func (d *careSampleDecoder) decode(raw []byte) care.Sample {
	var v struct {
		Awake       bool
		Unlocked    *bool
		IdleSeconds float64
		Application string
		Epoch       uint64
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		reason := diagnosticlog.DecodeReason(err)
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.write != nil && (d.at.IsZero() || time.Since(d.at) >= 5*time.Minute) {
			d.at = time.Now()
			d.write(diagnosticlog.Record{Level: "warn", Component: "desktop", Code: "native_presence_invalid", Reason: reason})
		}
		return care.Sample{}
	}
	return care.Sample{Presence: care.Presence{Awake: v.Awake, Unlocked: v.Unlocked}, Epoch: v.Epoch, IdleSeconds: v.IdleSeconds, Application: v.Application}
}
