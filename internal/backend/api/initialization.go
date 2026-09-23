package api

import "context"

// Initialization is a one-time user message, not a second source of identity.
type BotInitialization struct {
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}
type BotIntroduction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type BotInitializer interface {
	Initialization() BotInitialization
	RetryInitialization(context.Context) (BotInitialization, error)
	Initialize(context.Context, BotIntroduction) (BotInitialization, error)
}
