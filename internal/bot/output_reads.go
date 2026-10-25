package bot

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"slices"
	"strings"
)

// boundedData preserves the exact original JSON and its shape for small reads.
// Larger reads use revision-bound chunks, without repeating a mutation for output.
func boundedData(value map[string]any, q map[string]any) api.ToolResult {
	b := encoded(value["data"])
	if len(encoded(value)) <= 12<<10 && str(q, "cursor") == "" {
		return compactValue(value, false)
	}
	fingerprint := interactionFingerprint(value["data"])
	offset, err := interactionOffset(str(q, "cursor"), fingerprint, len(b))
	if err != nil {
		return compactError("cursor_invalid", err)
	}
	end := min(offset+4096, len(b))
	for end < len(b) && end > offset && (b[end]&0xc0) == 0x80 {
		end--
	}
	data := map[string]any{"encoding": "json-chunk", "json": string(b[offset:end]), "offset": offset, "totalBytes": len(b), "sha256": interactionFingerprint(value["data"]), "complete": end == len(b)}
	if end < len(b) {
		data["nextCursor"] = interactionToken(fingerprint, end)
	}
	result := map[string]any{}
	for k, v := range value {
		result[k] = v
	}
	result["data"] = data
	return compactValue(result, false)
}

// Bounded summary lists preserve a stable order and bind cursors to the filters.
func summaryPage(items []map[string]any, q map[string]any, identity string) api.ToolResult {
	slices.SortFunc(items, func(a, b map[string]any) int { return strings.Compare(str(a, identity), str(b, identity)) })
	fingerprint := interactionFingerprint(map[string]any{"type": q["type"], "archived": q["archived"], "items": items})
	offset, err := interactionOffset(str(q, "cursor"), fingerprint, len(items))
	if err != nil {
		return compactError("cursor_invalid", err)
	}
	limit := 20
	if n, ok := q["limit"].(float64); ok {
		limit = int(n)
	}
	end := offset
	for end < len(items) && end < offset+limit {
		if end > offset && len(encoded(items[offset:end+1])) > 16<<10 {
			break
		}
		end++
	}
	value := map[string]any{"ok": true, "data": items[offset:end], "total": len(items)}
	if end < len(items) {
		value["nextCursor"] = interactionToken(fingerprint, end)
	}
	return compactValue(value, false)
}
