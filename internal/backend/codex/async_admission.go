package codex

import "time"

// One fixed epoch per current native execution. Reads never create/renew it.
// Unknown historical calls without an epoch can restore a host-registered card,
// but cannot create a new actionable question after its tombstone is collected.
type asyncAdmission struct {
	Turn     string `json:"turn"`
	Deadline int64  `json:"deadline"`
}

func (a *asyncAdmission) begin(turn string) bool {
	if turn != "" && a.Turn != turn {
		a.Turn, a.Deadline = turn, time.Now().Add(24*time.Hour).Unix()
		return true
	}
	return false
}
func (a asyncAdmission) expiry(turn string) int64 {
	if a.Turn != turn || a.Deadline == 0 {
		return -1
	}
	return a.Deadline
}
