package tasks

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Display density is a native concern; active tasks have no display-count cap.
const CompletedRetention = 30 * time.Minute

func (m *Manager) notifyWatchlist() {
	m.mu.Lock()
	f := m.watchlistChanged
	m.mu.Unlock()
	if f != nil {
		f(m.TaskPreviews())
	}
}

// ClaimUnknownNotices records one alert per original execution before asking
// the native host to show it. A short grace lets automatic owner recovery finish;
// an uncertain task is never retried or replaced to produce this notice.
func (m *Manager) ClaimUnknownNotices() ([]string, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var claimed []string
	previous := map[string]string{}
	for id, r := range m.state.Records {
		if !m.owns(r) || r.View.Status != "unknown" || r.UnknownSince == 0 || m.now().Sub(time.UnixMilli(r.UnknownSince)) < 30*time.Second {
			continue
		}
		generation := r.Execution
		if generation == "" {
			generation = r.ReportID
		}
		if generation == "" {
			generation = id
		}
		key := hash(r.Provider, id, generation)
		if r.UnknownNotice == key {
			continue
		}
		previous[id] = r.UnknownNotice
		r.UnknownNotice = key
		claimed = append(claimed, id)
	}
	if len(claimed) > 0 {
		if err := m.write(); err != nil {
			for id, prior := range previous {
				m.state.Records[id].UnknownNotice = prior
			}
			return nil, err
		}
		sort.Strings(claimed)
	}
	return claimed, nil
}

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
	return DefaultMaxRunning
}
func (m *Manager) activeLocked() int {
	n := 0
	for _, r := range m.state.Records {
		if m.owns(r) && !terminal(r.View.Status) {
			n++
		}
	}
	return n
}
func summary(r *record) api.TaskSummary {
	return api.TaskSummary{ID: r.View.ID, Title: r.View.Title, Status: r.View.Status, Outcome: r.View.Outcome, Locked: r.Locked, Pinned: r.Pinned != nil && *r.Pinned}
}

// Legacy records acquire stable pagination order once. Only old unfinished work
// is migrated into the watchlist; historical records remain searchable.
func (m *Manager) metadataLocked() {
	ids := make([]string, 0, len(m.state.Records))
	for id, r := range m.state.Records {
		ids = append(ids, id)
		if r.Sequence > m.state.Sequence {
			m.state.Sequence = r.Sequence
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := m.state.Records[id]
		if r.Sequence == 0 {
			m.state.Sequence++
			r.Sequence = m.state.Sequence
		}
		if r.Pinned == nil {
			p := !terminal(r.View.Status)
			r.Pinned = &p
		}
		if !m.owns(r) {
			continue
		}
		if !terminal(r.View.Status) {
			r.CompletedAt = 0
		} else if r.CompletedAt == 0 {
			r.CompletedAt = m.now().UnixMilli()
		}
		if !r.Locked && r.CompletedAt > 0 && m.now().Sub(time.UnixMilli(r.CompletedAt)) >= CompletedRetention {
			pin := false
			r.Pinned = &pin
		}
	}
}

// RefreshWatchlist only reads adapter memory and ages the local ledger. No
// worker read, model turn, terminal launch or cancellation is dispatched.
func (m *Manager) RefreshWatchlist() error {
	m.op.Lock()
	err := m.refresh()
	m.op.Unlock()
	if err == nil {
		m.notifyWatchlist()
	}
	return err
}

type pageCursor struct {
	Before int64  `json:"before"`
	Filter string `json:"filter"`
}

func (m *Manager) QueryTasks(q api.TaskQuery) (api.TaskPage, error) {
	page := api.TaskPage{Tasks: []api.TaskSummary{}, CompletedRetentionSeconds: int(CompletedRetention.Seconds())}
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
		if !m.owns(r) || q.Status != "" && v.Status != q.Status || q.Pinned != nil && v.Pinned != *q.Pinned || q.Query != "" && !strings.Contains(strings.ToLower(v.ID+"\n"+v.Title+"\n"+r.OriginalPrompt), q.Query) {
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
	previous := *r
	r.Pinned = &pinned
	if !pinned {
		r.Locked = false
	}
	if pinned && terminal(r.View.Status) {
		r.CompletedAt = m.now().UnixMilli()
	}
	err := m.write()
	if err != nil {
		*r = previous
	}
	out := summary(r)
	m.mu.Unlock()
	if err == nil {
		m.notifyWatchlist()
	}
	return out, err
}

func (m *Manager) ObserveWatchlist(changed func([]api.TaskPreview)) {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchlistChanged = changed
}

// LockTask keeps a task resident without changing its native execution.
func (m *Manager) LockTask(id string, locked bool) (api.TaskSummary, error) {
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
	previous := *r
	r.Locked = locked
	if locked {
		pin := true
		r.Pinned = &pin
	}
	// Unlocking a completed task gives the user the normal grace period.
	if terminal(r.View.Status) {
		r.CompletedAt = m.now().UnixMilli()
	}
	err := m.write()
	if err != nil {
		*r = previous
	}
	out := summary(r)
	m.mu.Unlock()
	if err == nil {
		m.notifyWatchlist()
	}
	return out, err
}

// ClearTasks dismisses unlocked items, including active ones, until a new
// native execution or an explicit pin. Locks, history and workers survive.
func (m *Manager) ClearTasks() error {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return err
	}
	m.mu.Lock()
	previous := map[string]*bool{}
	for id, r := range m.state.Records {
		if m.owns(r) && !r.Locked && r.Pinned != nil && *r.Pinned {
			previous[id] = r.Pinned
			pin := false
			r.Pinned = &pin
		}
	}
	err := m.write()
	if err != nil {
		for id, pin := range previous {
			m.state.Records[id].Pinned = pin
		}
	}
	m.mu.Unlock()
	if err == nil {
		m.notifyWatchlist()
	}
	return err
}

// Manual ordering is independent of the immutable history pagination sequence.
// New native executions go to the front; ordinary polling preserves the order.
func (m *Manager) promoteWatchLocked(id string) {
	if order, ok := m.state.WatchOrder[m.provider]; ok {
		order = slices.DeleteFunc(slices.Clone(order), func(v string) bool { return v == id })
		m.state.WatchOrder[m.provider] = append([]string{id}, order...)
	}
}
func (m *Manager) MoveTask(id, before string) error {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.refresh(); err != nil {
		return err
	}
	previews := m.TaskPreviews()
	ids := make([]string, 0, len(previews))
	found, target := false, before == ""
	for _, p := range previews {
		if p.ID == id {
			found = true
			continue
		}
		ids = append(ids, p.ID)
		if p.ID == before {
			target = true
		}
	}
	if !found || !target || id == before {
		return errors.New("task order target is unavailable")
	}
	at := len(ids)
	if before != "" {
		at = slices.Index(ids, before)
	}
	ids = slices.Insert(ids, at, id)
	m.mu.Lock()
	if m.state.WatchOrder == nil {
		m.state.WatchOrder = map[string][]string{}
	}
	old, had := m.state.WatchOrder[m.provider]
	m.state.WatchOrder[m.provider] = ids
	err := m.write()
	if err != nil {
		if had {
			m.state.WatchOrder[m.provider] = old
		} else {
			delete(m.state.WatchOrder, m.provider)
		}
	}
	m.mu.Unlock()
	if err == nil {
		m.notifyWatchlist()
	}
	return err
}
