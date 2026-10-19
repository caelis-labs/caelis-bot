package bot

import (
	"embed"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Frozen metadata reconstructs historical catalog digests for unrelated pending
// callbacks. It is never advertised by the MCP server. Retired names have no
// executable handler, regardless of the old schema or approval metadata.
//
//go:embed catalog_history/*.json
var historicalCatalogs embed.FS

func archivedCatalog(name string) []api.ToolDefinition {
	raw, err := historicalCatalogs.ReadFile("catalog_history/" + name + ".json")
	if err != nil {
		panic(err)
	}
	var defs []api.ToolDefinition
	if err = json.Unmarshal(raw, &defs); err != nil {
		panic(err)
	}
	// ResultFormat is deliberately omitted from MCP JSON. All compact catalogs
	// used content-v1; the two original legacy catalogs used text-only results.
	if name != "legacy" && name != "legacy-memory" {
		for i := range defs {
			defs[i].ResultFormat = "content-v1"
		}
	}
	return defs
}
