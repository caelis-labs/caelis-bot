package productrpc

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ProfileBotID is the existing product projection of the persistent Bot identity.
// It creates no new identity and never transfers a native runtime session.
func ProfileBotID(rawID string) string { return api.ProfileBotID(rawID) }
