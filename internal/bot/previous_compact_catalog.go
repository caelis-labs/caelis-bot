package bot

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// Exact historical metadata, bound only to original pending callback versions.
func previousCompactDefinitions() []api.ToolDefinition { return archivedCatalog("5903254d") }
