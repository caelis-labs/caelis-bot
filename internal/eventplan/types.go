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
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Context  string    `json:"context,omitempty"`
	Kind     string    `json:"kind"` // wake or shell
	Command  string    `json:"command,omitempty"`
	When     string    `json:"when,omitempty"`
	TimeZone string    `json:"timeZone,omitempty"`
	Retain   bool      `json:"retain,omitempty"`
	Created  time.Time `json:"created"`
}

type Plan struct {
	ID       string    `json:"id"`
	Event    string    `json:"event"`
	Kind     string    `json:"kind"` // interval, after, at
	Seconds  int       `json:"seconds,omitempty"`
	At       string    `json:"at,omitempty"`
	When     string    `json:"when,omitempty"`
	TimeZone string    `json:"timeZone,omitempty"`
	Until    string    `json:"until,omitempty"`
	Created  time.Time `json:"created"`
	Next     time.Time `json:"next"`
	Expires  time.Time `json:"expires,omitempty"`
	Status   string    `json:"status"`
	Issue    string    `json:"issue,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
	// Retained only while needed to reconcile original deliveries.
	GrantID        string   `json:"grantId,omitempty"`
	OriginRequests []string `json:"originRequests,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
}

type Occurrence struct {
	OriginRequests []string        `json:"originRequests,omitempty"`
	ID             string          `json:"id"`
	Plan           string          `json:"plan"`
	Event          string          `json:"event"`
	At             time.Time       `json:"at"`
	Status         string          `json:"status"`
	Result         json.RawMessage `json:"result,omitempty"`
	Issue          string          `json:"issue,omitempty"`
	Finished       time.Time       `json:"finished,omitempty"`
}

type State struct {
	Version     int          `json:"version"`
	Migration   *Migration   `json:"migration,omitempty"`
	Events      []Event      `json:"events"`
	Plans       []Plan       `json:"plans"`
	Occurrences []Occurrence `json:"occurrences"`
}

// Runner is cancellable and bounded. Ambiguous effects cannot be retried.
type Runner func(context.Context, Event) (json.RawMessage, error)

type Wake struct {
	ID, Text, Plan, Grant string
	Deadline              time.Time
	OriginRequests        []string
}

type Delivery struct {
	Now     func() time.Time // refresh the clock at wake admission
	Revoke  func(string) error
	Ready   func() bool
	Submit  func(context.Context, Wake) (string, error)
	Receipt func(string) string
	// Settled receives only IDs with a committed terminal wake result. The
	// occurrence ledger now owns their deduplication.
	Settled func([]string)
}
