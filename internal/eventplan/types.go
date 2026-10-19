package eventplan

import (
	"context"
	"encoding/json"
	"time"
)

const MaxActive = 32
const MaxHistory = 128
const Retention = 7 * 24 * time.Hour

type Event struct {
	Retired          bool            `json:"retired,omitempty"`
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	Context          string          `json:"context,omitempty"`
	Kind             string          `json:"kind"` // script, tool, wake
	Script           string          `json:"script,omitempty"`
	Interpreter      string          `json:"interpreter,omitempty"`
	ExecutableDigest string          `json:"executableDigest,omitempty"`
	Server           string          `json:"server,omitempty"`
	Tool             string          `json:"tool,omitempty"`
	TaskExecution    string          `json:"taskExecution,omitempty"`
	Task             string          `json:"task,omitempty"`
	Arguments        map[string]any  `json:"arguments,omitempty"`
	Binding          string          `json:"binding,omitempty"`
	OutputSchema     json.RawMessage `json:"outputSchema"`
	Created          time.Time       `json:"created"`
	LastUsed         time.Time       `json:"lastUsed"`
	Archived         time.Time       `json:"archived,omitempty"`
}
type Plan struct {
	Retired         bool      `json:"retired,omitempty"`
	ID              string    `json:"id"`
	Event           string    `json:"event"`
	Kind            string    `json:"kind"` // interval, after, at
	Seconds         int       `json:"seconds,omitempty"`
	At              string    `json:"at,omitempty"`
	When            string    `json:"when,omitempty"`
	TimeZone        string    `json:"timeZone"`
	Until           string    `json:"until,omitempty"`
	CooldownSeconds int       `json:"cooldownSeconds,omitempty"`
	Created         time.Time `json:"created"`
	Next            time.Time `json:"next"`
	Expires         time.Time `json:"expires"`
	LastWake        time.Time `json:"lastWake,omitempty"`
	Status          string    `json:"status"`
	Issue           string    `json:"issue,omitempty"`
	Finished        time.Time `json:"finished,omitempty"`
	Failures        int       `json:"failures,omitempty"`
}
type Occurrence struct {
	AttentionFor string          `json:"attentionFor,omitempty"`
	ID           string          `json:"id"`
	Plan         string          `json:"plan"`
	Event        string          `json:"event"`
	At           time.Time       `json:"at"`
	Status       string          `json:"status"` // collecting, unknown, filtered, pending, dispatching, accepted, completed, expired, failed, cancelled
	Result       json.RawMessage `json:"result,omitempty"`
	Issue        string          `json:"issue,omitempty"`
	Finished     time.Time       `json:"finished,omitempty"`
}
type State struct {
	WakeAttempts []time.Time  `json:"wakeAttempts,omitempty"`
	Version      int          `json:"version"`
	Events       []Event      `json:"events"`
	Plans        []Plan       `json:"plans"`
	Occurrences  []Occurrence `json:"occurrences"`
}

// Runner executes the exact reviewed, pinned collector. It must be cancellable,
// bounded, and must not silently retry a call with uncertain effects.
type Runner func(context.Context, Event) (json.RawMessage, error)
type Delivery struct {
	Revoke   func(string) error
	Ready    bool
	Submit   func(context.Context, string, string, string) (string, error)
	Receipt  func(string) string
	Complete func(string) bool
}

// A failure returned with a confirmed result is different from a lost transport.
// Implementations expose this marker without leaking diagnostic text to users.
type collectorFailure string

func (e collectorFailure) Error() string        { return string(e) }
func (collectorFailure) ConfirmedFailure() bool { return true }
