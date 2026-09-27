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
	case "lock", "unlock", "clear":
		watchlist, ok := provider.(api.TaskWatchlist)
		if !ok {
			return nil, errors.New("task watchlist unavailable")
		}
		if in.Operation == "clear" {
			if err := watchlist.ClearTasks(); err != nil {
				return nil, err
			}
			pinned := true
			return catalog.QueryTasks(api.TaskQuery{Pinned: &pinned})
		}
		return watchlist.LockTask(in.ID, in.Operation == "lock")
	default:
		return nil, errors.New("unsupported task catalog operation")
	}
}
func taskCatalogSpec() any {
	return map[string]any{"name": "bot_tasks", "description": "Search this Bot's task history or manage its desktop background-task list. list defaults to 20 items (max 50); use nextCursor with the same filters. Responses omit transcripts and include the running admission limit (default 6). The display list has no capacity limit: new or resumed executions appear automatically, running tasks first. Completed unlocked items leave after 30 minutes. pin restores an item; unpin removes it and its lock until a new execution, without stopping work. lock pins and protects an item from automatic cleanup and clear; unlock starts the normal completed-item grace period. clear removes all unlocked items, including running items, without stopping workers or deleting history. Preserve explicit user removals during ordinary polling. Never adopt unrelated conversations.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"operation": map[string]any{"type": "string", "enum": []string{"list", "pin", "unpin", "lock", "unlock", "clear"}},
		"id":        map[string]any{"type": "string", "description": "Owned task handle for pin/unpin/lock/unlock"},
		"query":     map[string]any{"type": "string", "maxLength": 500, "description": "Case-insensitive title, assignment or handle search"},
		"status":    map[string]any{"type": "string", "description": "Optional exact task status filter"},
		"pinned":    map[string]any{"type": "boolean", "description": "Optional list filter"},
		"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
		"cursor":    map[string]any{"type": "string", "description": "Opaque nextCursor returned by the previous page with the same filters"},
	}}}
}
