// Package chatlog owns the Bot's IM transcript. Chat reads and older-page loads
// use this database; observed current Runtime output can update it. Tool output
// is never stored here. Disk errors retain the live cache;
// the writer retries independently of input, approval and connection control.
package chatlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	_ "modernc.org/sqlite"
)

const pageSize = 200

type Log struct {
	mu           sync.Mutex
	path         string
	items        []api.Item
	dirty        map[string]api.Item
	loaded       bool
	earlier      bool
	before       int64
	cancel       context.CancelFunc
	done         chan struct{}
	issue        error
	revision     uint64
	closeOnce    sync.Once
	visibleLimit int
}

func Open(path string) *Log {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Log{path: path, dirty: map[string]api.Item{}, visibleLimit: pageSize, cancel: cancel, done: make(chan struct{})}
	go l.run(ctx)
	return l
}

func key(i api.Item) string {
	if i.Kind == "user" && i.RequestID != "" {
		return "input:" + i.RequestID
	}
	return i.ID
}
func copyItem(i api.Item) api.Item {
	i.Details = ""
	i.Activity = nil
	i.Artifacts = append([]api.Artifact(nil), i.Artifacts...)
	if i.Screen != nil {
		screen := *i.Screen
		screen.Images = append([]api.ScreenImage(nil), screen.Images...)
		i.Screen = &screen
	}
	if i.Media != nil {
		media := *i.Media
		media.Images = append([]api.MediaImage(nil), media.Images...)
		i.Media = &media
	}
	return i
}
func (l *Log) Observe(items []api.Item) {
	if l == nil {
		return
	}
	l.mu.Lock()
	for _, item := range items {
		if item.Kind != "user" && item.Kind != "assistant" && item.Kind != "controlNotice" || item.ID == "" {
			continue
		}
		item = copyItem(item)
		found := false
		for n, old := range l.items {
			if key(old) != key(item) {
				continue
			}
			found = true
			if item.Kind == "user" {
				nativeEcho := !strings.HasPrefix(item.ID, "outgoing:") && !strings.HasPrefix(item.ID, "channel-input:") && !strings.HasPrefix(item.ID, "control-input:")
				if nativeEcho && old.Text != "" {
					item.Text = old.Text // model-only quote wrapping never replaces the visible user body
				}
				item.ID = old.ID // one display identity across channel receipt and native echo
				if item.Media == nil {
					item.Media = old.Media
				}
			}
			if old.SeenAt == 0 || item.SeenAt == 0 || old.SeenAt < item.SeenAt {
				item.SeenAt = old.SeenAt
			}
			if same(old, item) {
				break
			}
			l.items[n] = item
			l.dirty[key(item)] = item
			l.revision++
			break
		}
		if !found {
			if item.SeenAt == 0 && item.Kind != "controlNotice" {
				item.SeenAt = time.Now().UnixMicro()
			}
			l.items = append(l.items, item)
			l.dirty[key(item)] = item
			l.revision++
		}
	}
	l.mu.Unlock()
}
func same(a, b api.Item) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func (l *Log) Snapshot() ([]api.Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]api.Item, len(l.items))
	for n, i := range l.items {
		out[n] = copyItem(i)
	}
	return out, l.earlier
}
func (l *Log) Revision() uint64 { l.mu.Lock(); defer l.mu.Unlock(); return l.revision }
func (l *Log) Close() {
	if l != nil {
		l.closeOnce.Do(func() { l.cancel(); <-l.done })
	}
}
func (l *Log) open(ctx context.Context) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", l.path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA busy_timeout=1500; CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, body BLOB NOT NULL);`); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(l.path, 0600)
	return db, nil
}
func (l *Log) load(ctx context.Context, db *sql.DB, before int64) error {
	rows, err := db.QueryContext(ctx, `SELECT rowid,body FROM messages WHERE (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT ?`, before, before, pageSize+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	var older []api.Item
	var positions []int64
	earliest := before
	for rows.Next() {
		var pos int64
		var b []byte
		if err = rows.Scan(&pos, &b); err != nil {
			return err
		}
		var item api.Item
		if json.Unmarshal(b, &item) != nil || item.ID == "" || item.Kind != "user" && item.Kind != "assistant" && item.Kind != "controlNotice" {
			continue
		}
		older = append(older, copyItem(item))
		positions = append(positions, pos)
		earliest = pos
	}
	if err = rows.Err(); err != nil {
		return err
	}
	more := len(older) > pageSize
	if more {
		older = older[:pageSize]
	}
	if len(older) > 0 {
		earliest = positions[len(older)-1]
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	known := map[string]int{}
	for n, i := range l.items {
		known[key(i)] = n
	}
	prefix := make([]api.Item, 0, len(older))
	for n := len(older) - 1; n >= 0; n-- {
		previous := older[n]
		if pos, exists := known[key(previous)]; exists {
			// An observed current item may arrive before the disk cache loads.
			// Keep its original presentation position and retain the live text.
			if l.items[pos].SeenAt != previous.SeenAt {
				l.items[pos].SeenAt = previous.SeenAt
				l.dirty[key(previous)] = l.items[pos]
				l.revision++
			}
		} else {
			prefix = append(prefix, previous)
		}
	}
	l.items = append(prefix, l.items...)
	l.before = earliest
	l.earlier = more
	l.loaded = true
	l.revision++
	return nil
}
func (l *Log) LoadEarlier(ctx context.Context) error {
	l.mu.Lock()
	before, more := l.before, l.earlier
	l.mu.Unlock()
	if !more {
		return nil
	}
	l.mu.Lock()
	l.visibleLimit += pageSize
	l.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	db, err := l.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return l.load(ctx, db, before)
}
func (l *Log) flush(ctx context.Context) error {
	l.mu.Lock()
	loaded, empty := l.loaded, len(l.dirty) == 0
	l.mu.Unlock()
	if loaded && empty {
		return nil
	}
	db, err := l.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if !loaded {
		if err = l.load(ctx, db, 0); err != nil {
			return err
		}
	}
	l.mu.Lock()
	dirty := make(map[string]api.Item, len(l.dirty))
	for k, v := range l.dirty {
		dirty[k] = v
	}
	l.mu.Unlock()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Preserve observed order, rather than map iteration, for newly accepted IM messages.
	l.mu.Lock()
	order := append([]api.Item(nil), l.items...)
	l.mu.Unlock()
	for _, item := range order {
		k := key(item)
		value, ok := dirty[k]
		if !ok {
			continue
		}
		b, _ := json.Marshal(value)
		if _, err = tx.ExecContext(ctx, `INSERT INTO messages(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, k, b); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	l.mu.Lock()
	for k, v := range dirty {
		if current, ok := l.dirty[k]; ok && same(current, v) {
			delete(l.dirty, k)
		}
	}
	l.mu.Unlock()
	// Keep the default visible window small after persistence. Explicit local
	// history loading expands that window; Runtime history is never traversed.
	l.mu.Lock()
	if len(l.dirty) == 0 && len(l.items) > l.visibleLimit {
		l.items = append([]api.Item(nil), l.items[len(l.items)-l.visibleLimit:]...)
		l.earlier = true
		l.revision++
		first := key(l.items[0])
		l.mu.Unlock()
		var before int64
		if db.QueryRowContext(ctx, `SELECT rowid FROM messages WHERE id=?`, first).Scan(&before) == nil {
			l.mu.Lock()
			l.before = before
			l.mu.Unlock()
		}
	} else {
		l.mu.Unlock()
	}
	return nil
}
func (l *Log) Status() error { l.mu.Lock(); defer l.mu.Unlock(); return l.issue }
func (l *Log) run(ctx context.Context) {
	defer close(l.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		work, stop := context.WithTimeout(context.Background(), 2*time.Second)
		err := l.flush(work)
		stop()
		var sqliteError interface{ Code() int }
		if errors.As(err, &sqliteError) && (sqliteError.Code()&255 == 11 || sqliteError.Code()&255 == 26) {
			// This database contains display messages only. Preserve corrupt bytes
			// for diagnosis and rebuild the cache; native identities/receipts stay
			// in their original owner journals and are never reset here.
			if os.Rename(l.path, l.path+fmt.Sprintf(".damaged-%d", time.Now().UnixNano())) == nil {
				l.mu.Lock()
				l.loaded = false
				for _, i := range l.items {
					l.dirty[key(i)] = i
				}
				l.mu.Unlock()
			}
		}
		l.mu.Lock()
		l.issue = err
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			work, stop := context.WithTimeout(context.Background(), 2*time.Second)
			_ = l.flush(work)
			stop()
			return
		case <-ticker.C:
		}
	}
}
