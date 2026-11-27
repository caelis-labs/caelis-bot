package chatlog

import (
	"context"
	"encoding/json"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ClaimCardInput persists the original card input before dispatch. A process
// dying between the claim and the receipt leaves an unknown effect, even when
// the user reopens the card on another runtime or outside the recent page.
func (l *Log) ClaimCardInput(ctx context.Context, item api.Item) (prior *api.Item, err error) {
	// Commit earlier chat rows first so the input loads after its originating reply.
	if err := l.flush(ctx); err != nil {
		return nil, err
	}
	l.flushMu.Lock()
	defer l.flushMu.Unlock()
	db, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	body, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	result, err := db.ExecContext(ctx, "INSERT INTO messages(id,body) VALUES(?,?) ON CONFLICT(id) DO NOTHING", key(item), body)
	if err != nil {
		return nil, err
	}
	if inserted, err := result.RowsAffected(); err != nil || inserted != 0 {
		return nil, err
	}
	var previous api.Item
	if err := db.QueryRowContext(ctx, "SELECT body FROM messages WHERE id=?", key(item)).Scan(&body); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &previous); err != nil {
		return nil, err
	}
	return &previous, nil
}
