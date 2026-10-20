// Package managedupdate owns one managed installation, not Bot work or Runtime
// recovery. Browser commands contain opaque identities, never executable paths.
package managedupdate

import "errors"

const Protocol = 1

var (
	Source    = "unknown" // Explicitly set by the Linux package build.
	BuildKind = "development"
)

type Identity struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	Kind    string `json:"kind"`
}

type Status struct {
	Protocol      int      `json:"protocol"`
	Mode          string   `json:"mode"`
	State         string   `json:"state"`
	Reason        string   `json:"reason,omitempty"`
	Current       Identity `json:"current"`
	BootID        string   `json:"bootId,omitempty"`
	OfferID       string   `json:"offerId,omitempty"`
	Latest        string   `json:"latest,omitempty"`
	TransactionID string   `json:"transactionId,omitempty"`
	Revision      uint64   `json:"revision"`
	Received      int64    `json:"receivedBytes"`
	Total         int64    `json:"totalBytes"`
	Automatic     bool     `json:"automatic"`
	Scheduled     bool     `json:"scheduled"`
	TestArtifact  bool     `json:"testArtifact"`
	Runtime       Runtime  `json:"runtime"`
}

// Only observed, non-secret identity is exposed. Pin and running Host may differ
// while Core waits for idle or preserves a newer Host; Bot never downgrades it.
type Runtime struct {
	State          string `json:"state"`
	Reason         string `json:"reason,omitempty"`
	BundledVersion string `json:"bundledVersion,omitempty"`
	HostVersion    string `json:"hostVersion,omitempty"`
	HostBuild      string `json:"hostBuild,omitempty"`
	HostInstance   string `json:"hostInstance,omitempty"`
}

// ID is the original ingress ID, retained across timeout/reload. Revision is a
// CAS for cancellation; the offer and transaction are server-issued identities.
type Command struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	OfferID       string `json:"offerId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
	Revision      uint64 `json:"revision,omitempty"`
	When          string `json:"when,omitempty"` // now or idle
	Automatic     bool   `json:"automatic,omitempty"`
}

type Receipt struct {
	ID            string `json:"id"`
	TransactionID string `json:"transactionId,omitempty"`
	State         string `json:"state"` // accepted is not installed
}

type Error struct{ Code string }

func (e *Error) Error() string  { return e.Code }
func problem(code string) error { return &Error{Code: code} }
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "update_unconfirmed"
}

type Owner interface {
	Status() Status
	Command(Command) (Receipt, error)
	Receipt(string) (Receipt, error)
}

type Boot struct {
	ID       string   `json:"id"`
	PID      int      `json:"pid"`
	Identity Identity `json:"identity"`
	Runtime  Runtime  `json:"runtime,omitempty"`
}

type Handoff struct {
	BootID        string `json:"bootId"`
	TransactionID string `json:"transactionId"`
	State         string `json:"state"` // requested, busy, committed, failed
	Reason        string `json:"reason,omitempty"`
}
