package api

import "context"

// Personal data is application-owned and shared across execution providers.
// These are product projections, never Memory credentials or domain selectors.
type MemoryEntry struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Source  string `json:"source"`
	Created string `json:"created"`
}
type PersonalMemory struct {
	Evidence  []MemoryEntry `json:"evidence"`
	Truncated bool          `json:"truncated"`
}
type MemoryChange struct {
	RequestID string `json:"requestId"`
	ID        string `json:"id"`
	Text      string `json:"text"`
}
type PersonalTools interface {
	ReadMemory(context.Context, string) (PersonalMemory, error)
	Remember(context.Context, string, string, string) (MemoryEntry, error)
	CorrectMemory(context.Context, MemoryChange) error
	ForgetMemory(context.Context, MemoryChange) error
}
