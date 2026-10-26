package chatlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type questionOutcome struct{ binding, outcome string }

// ResolveQuestionReply preserves the original display and changes only the
// outcome supported by the original controller's receipt. Pending patches also
// cover receipts arriving before the asynchronous transcript load, or for old
// messages outside the loaded page.
func (l *Log) ResolveQuestionReply(request, binding, outcome string) {
	if l == nil || request == "" || binding == "" || (outcome != "accepted" && outcome != "rejected") {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.questionOutcomes == nil {
		l.questionOutcomes = map[string]questionOutcome{}
	}
	value := questionOutcome{binding, outcome}
	l.questionOutcomes[request] = value
	for n := range l.items {
		if l.items[n].RequestID == request && applyQuestionOutcome(&l.items[n], value) {
			l.dirty[key(l.items[n])] = l.items[n]
		}
	}
	l.revision++
}
func applyQuestionOutcome(item *api.Item, value questionOutcome) bool {
	if item.QuestionReply == nil || item.QuestionReply.Binding != value.binding || item.QuestionReply.Outcome == value.outcome {
		return false
	}
	copy := *item.QuestionReply
	copy.Outcome = value.outcome
	item.QuestionReply, item.Status = &copy, value.outcome
	return true
}

// Native echoes and stale dispatch updates cannot overwrite a controller-owned
// response or regress a terminal receipt, including during the initial DB merge.
func preserveQuestionReply(item *api.Item, previous api.Item) bool {
	old := previous.QuestionReply
	if old == nil {
		return false
	}
	if item.QuestionReply == nil {
		item.QuestionReply, item.Text, item.Status = old, previous.Text, previous.Status
		return true
	}
	if item.QuestionReply.Binding == old.Binding && (old.Outcome == "accepted" || old.Outcome == "rejected") && (item.QuestionReply.Outcome == "sending" || item.QuestionReply.Outcome == "unknown") {
		copy := *item.QuestionReply
		copy.Outcome = old.Outcome
		item.QuestionReply, item.Status = &copy, previous.Status
		return true
	}
	return false
}

func persistQuestionOutcomes(ctx context.Context, tx *sql.Tx, values map[string]questionOutcome) error {
	for request, value := range values {
		var body []byte
		err := tx.QueryRowContext(ctx, `SELECT body FROM messages WHERE id=?`, "input:"+request).Scan(&body)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		var item api.Item
		if json.Unmarshal(body, &item) != nil || !applyQuestionOutcome(&item, value) {
			continue
		}
		body, _ = json.Marshal(item)
		if _, err = tx.ExecContext(ctx, `UPDATE messages SET body=? WHERE id=?`, body, "input:"+request); err != nil {
			return err
		}
	}
	return nil
}
