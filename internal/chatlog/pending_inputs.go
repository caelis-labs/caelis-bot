package chatlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Only cache misses wait here, not the complete transcript. Failed reads retain
// their incoming items without exposing a possibly model-only native body.
// flushMu serializes reads with persistence; mu fences newer observations and
// receipt updates while the database read is in flight.
func (l *Log) resolvePendingInputs(ctx context.Context, db *sql.DB) error {
	l.mu.Lock()
	order := append([]string(nil), l.pendingOrder...)
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		remaining := l.pendingOrder[:0]
		for _, k := range l.pendingOrder {
			if _, exists := l.pendingInputs[k]; exists {
				remaining = append(remaining, k)
			}
		}
		l.pendingOrder = remaining
	}()
	for _, k := range order {
		var body []byte
		err := db.QueryRowContext(ctx, `SELECT body FROM messages WHERE id=?`, k).Scan(&body)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var previous api.Item
		if err == nil {
			if err := json.Unmarshal(body, &previous); err != nil {
				return err
			}
		}
		l.mu.Lock()
		if item, exists := l.pendingInputs[k]; exists {
			if previous.ID != "" {
				item = mergeObservedItem(item, previous)
			}
			l.observeLocked(item, false)
			delete(l.pendingInputs, k)
		}
		l.mu.Unlock()
	}
	return nil
}
