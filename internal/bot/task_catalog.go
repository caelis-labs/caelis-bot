package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func callTaskCatalog(provider api.TaskProvider, args json.RawMessage) (any, error) {
	catalog, ok := provider.(api.TaskCatalog)
	if !ok {
		return nil, errors.New("task catalog unavailable")
	}
	var in struct {
		Operation string `json:"operation"`
		ID        string `json:"id"`
		api.TaskQuery
	}
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	if len(args) > 4096 || d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid task catalog arguments")
	}
	switch in.Operation {
	case "", "list":
		return catalog.QueryTasks(in.TaskQuery)
	case "pin":
		return catalog.PinTask(in.ID, true)
	case "unpin":
		return catalog.PinTask(in.ID, false)
	default:
		return nil, errors.New("unsupported task catalog operation")
	}
}
func taskCatalogSpec() any {
	return map[string]any{"name": "bot_tasks", "description": "Search and paginate this Bot's task history, or pin/unpin an owned task in the desktop watchlist. History has no task-count cap. list defaults to 20 items (max 50); reuse nextCursor with the same filters. Search matches title, original assignment or task handle; results omit transcripts. The response includes the current running limit (user preference, default 3) and pin limit (8). New tasks are unpinned. Pin work that the user needs to follow; unpin never stops work or deletes history. A full watchlist is an explicit error; choose an item to unpin, never evict silently. These operations never adopt unrelated conversations.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"operation": map[string]any{"type": "string", "enum": []string{"list", "pin", "unpin"}},
		"id":        map[string]any{"type": "string", "description": "Owned task handle for pin/unpin"},
		"query":     map[string]any{"type": "string", "maxLength": 500, "description": "Case-insensitive title, assignment or handle search"},
		"status":    map[string]any{"type": "string", "description": "Optional exact task status filter"},
		"pinned":    map[string]any{"type": "boolean", "description": "Optional list filter"},
		"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
		"cursor":    map[string]any{"type": "string", "description": "Opaque nextCursor returned by the previous page with the same filters"},
	}}}
}
