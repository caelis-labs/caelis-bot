package bot

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Read projections only. Native records, IDs, decisions and receipts are unchanged.
type interactionEntry struct {
	Kind  string
	ID    string
	Value any
}
type interactionCursor struct {
	Fingerprint string
	Offset      int
}

func interactionOffset(token, fingerprint string, total int) (int, error) {
	if token == "" {
		return 0, nil
	}
	var c interactionCursor
	b, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || json.Unmarshal(b, &c) != nil || c.Fingerprint != fingerprint || c.Offset < 0 || c.Offset > total {
		return 0, errors.New("requests changed; restart the original read")
	}
	return c.Offset, nil
}
func interactionToken(fingerprint string, offset int) string {
	return base64.RawURLEncoding.EncodeToString(encoded(interactionCursor{fingerprint, offset}))
}
func interactionFingerprint(v any) string { return fmt.Sprintf("%x", sha256.Sum256(encoded(v))) }

// Only explicit terminal evidence removes a request from the todo projection.
// In particular sent/sending/dispatching are not native completion receipts.
func interactionTerminal(state string) bool {
	switch state {
	case "resolved", "answered", "expired", "dismissed", "superseded", "cancelled", "canceled", "completed":
		return true
	}
	return false
}
func interactionPage(port api.WorkerInteractionPort, q map[string]any) api.ToolResult {
	entries := []interactionEntry{}
	for _, a := range port.WorkerApprovals() {
		if !interactionTerminal(a.Status) {
			entries = append(entries, interactionEntry{"approval", a.ID, a})
		}
	}
	for _, v := range port.WorkerQuestions() {
		if !interactionTerminal(v.State) {
			entries = append(entries, interactionEntry{"question", v.ID, v})
		}
	}
	slices.SortFunc(entries, func(a, b interactionEntry) int { return strings.Compare(a.Kind+":"+a.ID, b.Kind+":"+b.ID) })
	fingerprint := interactionFingerprint(entries)
	offset, e := interactionOffset(str(q, "cursor"), fingerprint, len(entries))
	if e != nil {
		return compactError("cursor_invalid", e)
	}
	limit := 20
	if n, ok := q["limit"].(float64); ok {
		limit = int(n)
	}
	approvals, questions := []api.Approval{}, []api.WorkerQuestion{}
	reads := []map[string]string{}
	end := offset
	for end < len(entries) && end < offset+limit {
		v := entries[end]
		// A large original is never truncated or presented as a grant: expose its
		// exact handle and an executable lossless read instead.
		if len(encoded(v.Value)) > 12<<10 {
			reads = append(reads, map[string]string{"kind": v.Kind, "id": v.ID})
		} else if v.Kind == "approval" {
			approvals = append(approvals, v.Value.(api.Approval))
		} else {
			questions = append(questions, v.Value.(api.WorkerQuestion))
		}
		end++
		if len(encoded(map[string]any{"approvals": approvals, "questions": questions, "reads": reads})) > 16<<10 {
			break
		}
	}
	data := map[string]any{"approvals": approvals, "questions": questions, "reads": reads, "total": len(entries)}
	if end < len(entries) {
		data["nextCursor"] = interactionToken(fingerprint, end)
	}
	return compactValue(map[string]any{"ok": true, "data": data}, false)
}
func readInteraction(port api.WorkerInteractionPort, q map[string]any) api.ToolResult {
	id, kind := str(q, "id"), str(q, "kind")
	var value any
	if kind == "approval" {
		for _, a := range port.WorkerApprovals() {
			if a.ID == id {
				value = a
				break
			}
		}
	} else {
		for _, v := range port.WorkerQuestions() {
			if v.ID == id {
				value = v
				break
			}
		}
	}
	if value == nil {
		return compactError("unavailable", errors.New("original Worker request unavailable"))
	}
	b := encoded(value)
	if len(b) <= 12<<10 && str(q, "cursor") == "" {
		return compactValue(map[string]any{"ok": true, "data": value}, false)
	}
	// Page the exact renderer-safe native projection, not a paraphrase. JSON's
	// text is carried verbatim in UTF-8 chunks, including Unicode boundaries.
	fingerprint := interactionFingerprint(value)
	offset, e := interactionOffset(str(q, "cursor"), fingerprint, len(b))
	if e != nil {
		return compactError("cursor_invalid", e)
	}
	end := min(offset+4096, len(b))
	// UTF-8 JSON may contain literal Unicode. Never split one code point.
	for end < len(b) && end > offset && (b[end]&0xc0) == 0x80 {
		end--
	}
	data := map[string]any{"kind": kind, "id": id, "encoding": "json-chunk", "json": string(b[offset:end]), "offset": offset, "totalBytes": len(b), "sha256": fingerprint, "complete": end == len(b)}
	if end < len(b) {
		data["nextCursor"] = interactionToken(fingerprint, end)
	}
	return compactValue(map[string]any{"ok": true, "data": data}, false)
}
