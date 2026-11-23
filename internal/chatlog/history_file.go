package chatlog

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type HistoryFilter func(api.Item) bool

func conversationItem(item api.Item, exclude HistoryFilter) bool {
	return (item.Kind == "user" || item.Kind == "assistant") &&
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
	var known bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE id=?)", "input:"+request).Scan(&known); err != nil {
		return time.Time{}, false, err
	}
	if known {
		return time.Time{}, true, nil
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
			return time.UnixMicro(item.SeenAt), known, nil
		}
	}
	return time.Time{}, known, rows.Err()
}

// StartHistoryFile publishes a disposable read-only view of the committed IM.
// A dedicated SQLite connection tracks commits without polling the live UI cache.
func (l *Log) StartHistoryFile(path string, exclude HistoryFilter) {
	l.projectionOnce.Do(func() {
		l.projection.Go(func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			var db *sql.DB
			defer func() {
				if db != nil {
					_ = db.Close()
				}
			}()
			var published int64 = -1
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if db == nil {
					db, _ = l.open(ctx)
				}
				if db != nil {
					var version int64
					err := db.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version)
					_, stat := os.Stat(path)
					if err == nil && (published != version || stat != nil) {
						err = publishHistoryFile(ctx, db, path, exclude)
						if err == nil {
							published = version
						}
					}
					if err != nil {
						_ = db.Close()
						db, published = nil, -1
					}
				}
				cancel()
				select {
				case <-l.done:
					return
				case <-tick.C:
				}
			}
		})
	})
}

func publishHistoryFile(ctx context.Context, db *sql.DB, path string, exclude HistoryFilter) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".chat-history-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	rows, err := db.QueryContext(ctx, `SELECT id,body FROM messages WHERE json_valid(body)
		ORDER BY COALESCE(CAST(json_extract(body,'$.seenAt') AS INTEGER),0), rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	w := bufio.NewWriter(tmp)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var item api.Item
		if json.Unmarshal(raw, &item) != nil || !conversationItem(item, exclude) {
			continue
		}
		if item.Kind == "user" && item.Status != "accepted" && item.Status != "completed" {
			continue
		}
		type entry struct {
			ID     string `json:"id"`
			Role   string `json:"role"`
			Text   string `json:"text"`
			Status string `json:"status"`
			Time   string `json:"time,omitempty"`
		}
		row := entry{ID: id, Role: item.Kind, Text: item.Text, Status: item.Status}
		if item.SeenAt > 0 {
			row.Time = time.UnixMicro(item.SeenAt).UTC().Format(time.RFC3339Nano)
		}
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := tmp.Chmod(0400); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
