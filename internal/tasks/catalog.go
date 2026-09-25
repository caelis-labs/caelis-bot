package tasks

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const PinnedLimit = 8

// ConfigureLimit changes admission only; reducing the preference never stops work.
func (m *Manager) ConfigureLimit(read func() int) {
	m.op.Lock()
	defer m.op.Unlock()
	m.maxRunning = read
}
func (m *Manager) maximumRunning() int {
	if m.maxRunning != nil {
		if n := m.maxRunning(); n > 0 {
			return n
		}
	}
	return 3
}
func (m *Manager) activeLocked() int {
	n := 0
	for _, r := range m.state.Records {
		if r.Provider == m.provider && !terminal(r.View.Status) {
			n++
		}
	}
	return n
}
func (m *Manager) pinnedLocked() int {
	n := 0
	for _, r := range m.state.Records {
		if r.Provider == m.provider && r.Pinned != nil && *r.Pinned {
			n++
		}
	}
	return n
}
func summary(r *record) api.TaskSummary {
	return api.TaskSummary{ID: r.View.ID, Title: r.View.Title, Status: r.View.Status, Outcome: r.View.Outcome, Pinned: r.Pinned != nil && *r.Pinned}
}

// Legacy records acquire stable pagination order once. Only old unfinished work
// is migrated into the bounded watchlist; historical records remain searchable.
func (m *Manager) metadataLocked() {
	ids := make([]string, 0, len(m.state.Records))
	for id, r := range m.state.Records {
		ids = append(ids, id)
		if r.Sequence > m.state.Sequence {
			m.state.Sequence = r.Sequence
		}
	}
	sort.Strings(ids)
	pins := map[string]int{}
	for _, r := range m.state.Records {
		if r.Pinned != nil && *r.Pinned {
			pins[r.Provider]++
		}
	}
	for _, id := range ids {
		r := m.state.Records[id]
		if r.Sequence == 0 {
			m.state.Sequence++
			r.Sequence = m.state.Sequence
		}
		if r.Pinned == nil {
			p := !terminal(r.View.Status) && pins[r.Provider] < PinnedLimit
			r.Pinned = &p
			if p {
				pins[r.Provider]++
			}
		}
	}
}

type pageCursor struct {
	Before int64  `json:"before"`
	Filter string `json:"filter"`
}

func (m *Manager) QueryTasks(q api.TaskQuery) (api.TaskPage, error) {
	page := api.TaskPage{Tasks: []api.TaskSummary{}, PinnedLimit: PinnedLimit}
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Limit < 1 || q.Limit > 50 || len(q.Query) > 500 || len(q.Status) > 64 || len(q.Cursor) > 512 {
		return page, errors.New(m.text("host.taskQueryInvalid"))
	}
	q.Query = strings.ToLower(strings.TrimSpace(q.Query))
	pinned := "all"
	if q.Pinned != nil {
		if *q.Pinned {
			pinned = "true"
		} else {
			pinned = "false"
		}
	}
	filter := hash(m.provider, q.Query, q.Status, pinned)
	var cursor pageCursor
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(b, &cursor) != nil || cursor.Before < 1 || cursor.Filter != filter {
			return page, errors.New(m.text("host.taskCursorInvalid"))
		}
	}
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return page, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	page.Running = m.activeLocked()
	page.MaxRunning = m.maximumRunning()
	var matches []*record
	for _, r := range m.state.Records {
		v := summary(r)
		if r.Provider != m.provider || q.Status != "" && v.Status != q.Status || q.Pinned != nil && v.Pinned != *q.Pinned || q.Query != "" && !strings.Contains(strings.ToLower(v.ID+"\n"+v.Title+"\n"+r.OriginalPrompt), q.Query) {
			continue
		}
		page.Total++
		if cursor.Before == 0 || r.Sequence < cursor.Before {
			matches = append(matches, r)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Sequence > matches[j].Sequence })
	for i, r := range matches {
		if i == q.Limit {
			b, _ := json.Marshal(pageCursor{Before: matches[i-1].Sequence, Filter: filter})
			page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
			break
		}
		page.Tasks = append(page.Tasks, summary(r))
	}
	return page, nil
}

func (m *Manager) PinTask(id string, pinned bool) (api.TaskSummary, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return api.TaskSummary{}, err
	}
	if err := m.owned(id); err != nil {
		return api.TaskSummary{}, err
	}
	m.mu.Lock()
	r := m.state.Records[id]
	if *r.Pinned == pinned {
		out := summary(r)
		m.mu.Unlock()
		return out, nil
	}
	if pinned && m.pinnedLocked() >= PinnedLimit {
		m.mu.Unlock()
		return api.TaskSummary{}, errors.New(m.text("host.taskPinsFull"))
	}
	previous := r.Pinned
	r.Pinned = &pinned
	err := m.write()
	if err != nil {
		r.Pinned = previous
	}
	out := summary(r)
	m.mu.Unlock()
	if err == nil && m.watchlistChanged != nil {
		m.watchlistChanged(m.TaskPreviews())
	}
	return out, err
}

func (m *Manager) ObserveWatchlist(changed func([]api.TaskPreview)) {
	m.op.Lock()
	defer m.op.Unlock()
	m.watchlistChanged = changed
}
