package api

// RuntimeExperiments projects Core's shared Host defaults, not Bot preferences.
// SaveState describes the host-owned save flow: pending requires original-ID
// readback. A returned flow is not a claim that the configuration was saved.
type RuntimeExperiments struct {
	Supported   bool                `json:"supported"`
	Revision    string              `json:"revision"`
	Features    []RuntimeExperiment `json:"features"`
	SaveState   string              `json:"saveState"`
	OperationID string              `json:"operationId"`
}
type RuntimeExperiment struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type RuntimeExperimentChange struct {
	ID               string `json:"id"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision string `json:"expectedRevision"`
}
