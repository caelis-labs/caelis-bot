package api

import "context"

// ImageInputCapability describes the resident model, never a worker or the
// union of capabilities of all installed models. Unknown is not supported.
type ImageInputCapability struct {
	Model string `json:"model"`
	State string `json:"state"` // supported, unsupported, unknown
}

type ImageInputProvider interface {
	ImageInput(context.Context) (ImageInputCapability, error)
}
