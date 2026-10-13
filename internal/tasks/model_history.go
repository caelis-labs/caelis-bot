package tasks

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// QueryTaskHistory searches the durable ownership ledger, including tasks
// whose desktop cards have expired. A missing legacy creation time stays
// unknown; such entries sort after dated entries by durable sequence and ID.
// createdAfter is inclusive and createdBefore is exclusive.
func (m *Manager) QueryTaskHistory(q api.TaskHistoryQuery) ([]api.TaskHistoryEntry, error) {
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Limit < 1 || q.Limit > 50 || q.Offset < 0 || q.Offset > 1000000 || len(q.Query) > 500 || len(q.Status) > 64 || q.Order != "" && q.Order != "asc" && q.Order != "desc" {
		return nil, errors.New("invalid task history query")
	}
	var after, before time.Time
	var err error
	if q.CreatedAfter != "" {
		after, err = time.Parse(time.RFC3339Nano, q.CreatedAfter)
		if err != nil {
			return nil, errors.New("createdAfter must be an RFC3339 timestamp with timezone")
		}
	}
	if q.CreatedBefore != "" {
		before, err = time.Parse(time.RFC3339Nano, q.CreatedBefore)
		if err != nil {
			return nil, errors.New("createdBefore must be an RFC3339 timestamp with timezone")
		}
	}
	if !after.IsZero() && !before.IsZero() && !after.Before(before) {
		return nil, errors.New("createdAfter must precede createdBefore")
	}
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	query := strings.ToLower(strings.TrimSpace(q.Query))
	type match struct {
		entry             api.TaskHistoryEntry
		created, sequence int64
	}
	var matches []match
	for _, r := range m.state.Records {
		if !m.owns(r) || q.Status != "" && r.View.Status != q.Status || query != "" && !strings.Contains(strings.ToLower(r.View.ID+"\n"+r.View.Title+"\n"+r.OriginalPrompt), query) {
			continue
		}
		if !after.IsZero() && (r.CreatedAt == 0 || time.UnixMilli(r.CreatedAt).Before(after)) || !before.IsZero() && (r.CreatedAt == 0 || !time.UnixMilli(r.CreatedAt).Before(before)) {
			continue
		}
		environment := "local"
		if r.View.Machine != "" {
			environment = r.View.Machine
		}
		entry := api.TaskHistoryEntry{ID: r.View.ID, Title: r.View.Title, Status: r.View.Status, Environment: environment}
		if r.CreatedAt != 0 {
			entry.CreatedAt = time.UnixMilli(r.CreatedAt).UTC().Format(time.RFC3339Nano)
		}
		matches = append(matches, match{entry: entry, created: r.CreatedAt, sequence: r.Sequence})
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if (a.created == 0) != (b.created == 0) {
			return a.created != 0
		}
		if a.created != b.created {
			return a.created > b.created && q.Order != "asc" || a.created < b.created && q.Order == "asc"
		}
		if a.sequence != b.sequence {
			return a.sequence > b.sequence && q.Order != "asc" || a.sequence < b.sequence && q.Order == "asc"
		}
		return a.entry.ID < b.entry.ID
	})
	if q.Offset >= len(matches) {
		return []api.TaskHistoryEntry{}, nil
	}
	end := min(len(matches), q.Offset+q.Limit)
	out := make([]api.TaskHistoryEntry, 0, end-q.Offset)
	for _, item := range matches[q.Offset:end] {
		out = append(out, item.entry)
	}
	return out, nil
}

func (m *Manager) ReadTaskConversation(ctx context.Context, id string) (api.TaskConversation, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.owned(id); err != nil {
		return api.TaskConversation{}, err
	}
	reader, ok := m.work.(api.WorkConversationReader)
	if !ok {
		return api.TaskConversation{}, errors.New("native task messages are unavailable for this Runtime")
	}
	view, err := m.work.ReadWork(ctx, id)
	view, err = m.capture(id, view, err)
	if err != nil {
		return api.TaskConversation{}, err
	}
	messages, err := reader.ReadWorkConversation(ctx, id)
	if err != nil {
		return api.TaskConversation{}, err
	}
	if messages == nil {
		messages = []api.TaskConversationMessage{}
	}
	return api.TaskConversation{Status: view.Status, Messages: messages}, nil
}

var _ api.TaskHistoryCatalog = (*Manager)(nil)
var _ api.TaskConversationReader = (*Manager)(nil)
