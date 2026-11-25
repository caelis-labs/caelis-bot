package chatlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type HistoryFilter func(api.Item) bool

func conversationItem(item api.Item, exclude HistoryFilter) bool {
	return (item.Kind == "user" || item.Kind == "assistant") &&
		// The shared control handler records its own accepted receipts in
		// this namespace. They never represent an admitted model message.
		(item.Kind != "user" || item.ID != "control-input:"+item.RequestID) &&
		(item.RemoteCommand == nil || !*item.RemoteCommand) && item.QuestionReply == nil &&
		(exclude == nil || !exclude(item))
}

// ConversationActivity reads the committed IM, including rows outside the
// loaded chat page. A recurring native input ID keeps its first display time.
func (l *Log) ConversationActivity(ctx context.Context, request string, exclude HistoryFilter) (time.Time, bool, error) {
	if err := l.flush(ctx); err != nil {
		return time.Time{}, false, err
	}
	db, err := l.open(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	defer db.Close()
	var recorded []byte
	err = db.QueryRowContext(ctx, "SELECT body FROM messages WHERE id=?", "input:"+request).Scan(&recorded)
	if err == nil {
		var item api.Item
		if json.Unmarshal(recorded, &item) == nil && conversationItem(item, exclude) && settledInput(item.Status) {
			return time.Time{}, true, nil
		}
	} else if err != sql.ErrNoRows {
		return time.Time{}, false, err
	}
	rows, err := db.QueryContext(ctx, `SELECT body FROM messages
		WHERE CASE WHEN json_valid(body) THEN json_extract(body,'$.kind') = 'user' ELSE 0 END
		ORDER BY CAST(json_extract(body,'$.seenAt') AS INTEGER) DESC, rowid DESC`)
	if err != nil {
		return time.Time{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return time.Time{}, false, err
		}
		var item api.Item
		if json.Unmarshal(raw, &item) == nil && conversationItem(item, exclude) && item.SeenAt > 0 &&
			(item.Status == "accepted" || item.Status == "completed") {
			return time.UnixMicro(item.SeenAt), false, nil
		}
	}
	return time.Time{}, false, rows.Err()
}

// The channel's incoming receipt precedes Runtime admission. Native receipts
// (including an uncertain original attempt) preserve request identity on retry.
func settledInput(status string) bool {
	switch status {
	case "accepted", "completed", "unknown", "rejected":
		return true
	}
	return false
}
