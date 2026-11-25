package chatlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	historyArchiveInterval = 5 * time.Minute
	archiveDayMicros       = int64(24 * time.Hour / time.Microsecond)
	archiveBaseline        = int64(-1)
	archiveDateLayout      = "2006/01/02"
)

// StartHistoryArchive projects committed conversation text on startup and then
// in five-minute batches. Closing the log cancels and joins the projector.
func (l *Log) StartHistoryArchive(root string, exclude HistoryFilter) {
	l.projectionOnce.Do(func() {
		l.projection.Go(func() {
			ticks := time.NewTicker(historyArchiveInterval)
			defer ticks.Stop()
			l.runHistoryArchive(root, exclude, ticks.C)
		})
	})
}

// ArchiveIssue exposes only whether the most recent batch needs recovery.
func (l *Log) ArchiveIssue() bool { return l.archiveIssue.Load() }

func (l *Log) runHistoryArchive(root string, exclude HistoryFilter, ticks <-chan time.Time) {
	archive := historyArchive{root: root, location: time.Local, exclude: exclude}
	ready := false
	for l.ctx.Err() == nil {
		var err error
		if !ready {
			err = l.flush(l.ctx) // Open the IM and commit any startup observations.
			ready = err == nil
		}
		if ready {
			// Reopen per batch so recovery can replace a damaged display database.
			// Unlike Log.open, this performs no schema setup or chmod on idle ticks.
			var db *sql.DB
			db, err = sql.Open("sqlite", sqliteDatabaseURI(l.path, "rw"))
			if err == nil {
				db.SetMaxOpenConns(1)
				if _, err = db.ExecContext(l.ctx, "PRAGMA busy_timeout=1500"); err == nil {
					err = archive.sync(l.ctx, db) // Outstanding dates stay queued for the next batch.
				}
				_ = db.Close()
			}
		}
		l.archiveIssue.Store(err != nil)
		select {
		case <-l.ctx.Done():
			return
		case <-ticks:
		}
	}
}

type historyArchive struct {
	root     string
	location *time.Location
	exclude  HistoryFilter
	calendar string
	repair   map[int64]struct{}
	retired  bool
}

func (a *historyArchive) sync(ctx context.Context, db *sql.DB) error {
	if a.calendar == "" {
		a.calendar = archiveCalendarKey(a.location)
	}
	if err := a.prepare(ctx, db); err != nil {
		return err
	}
	if a.repair == nil {
		repair, err := a.missingDays(ctx, db)
		if err != nil {
			return err
		}
		a.repair = repair
	}
	pending, err := pendingArchiveBuckets(ctx, db)
	if err != nil {
		return err
	}
	days := make(map[int64]struct{}, len(a.repair)+len(pending))
	for day := range a.repair {
		days[day] = struct{}{}
	}
	for _, bucket := range pending {
		for _, day := range a.bucketDays(bucket) {
			days[day] = struct{}{}
		}
	}
	var failed error
	published := make(map[int64]bool, len(days))
	for day := range days {
		if err = ctx.Err(); err != nil {
			return err
		}
		body, readErr := a.readDay(ctx, db, day)
		if readErr == nil {
			readErr = publishArchiveDay(a.path(day), body)
		}
		if readErr != nil {
			failed = errors.Join(failed, readErr)
			continue
		}
		published[day] = len(body) > 0
	}
	if len(published) != 0 {
		if err = a.acknowledge(ctx, db, pending, published); err != nil {
			return errors.Join(failed, err)
		}
		for day := range published {
			delete(a.repair, day)
		}
	}
	if failed == nil && !a.retired {
		failed = removeArchiveFile(filepath.Join(a.root, "conversation.jsonl"))
		a.retired = failed == nil
	}
	return failed
}

// The UTC change bucket is stable across OSes and SQLite connections. It
// records the range of changed timestamps, so a neighboring local date
// participates only if that range crosses its boundary.
func (a *historyArchive) bucketDays(bucket archiveBucket) []int64 {
	if bucket.start == archiveBaseline || a.location == time.UTC {
		return []int64{bucket.start}
	}
	start := time.UnixMicro(bucket.first).In(a.location)
	end := time.UnixMicro(bucket.last).In(a.location)
	var days []int64
	// Include both ends of each fixed-offset segment, including a DST change
	// within the bucket that briefly crosses a calendar boundary.
	for {
		_, transition := start.ZoneBounds()
		if !transition.After(start) || transition.After(end) {
			days = append(days, localArchiveDay(start), localArchiveDay(end))
			return days
		}
		days = append(days, localArchiveDay(start), localArchiveDay(transition.Add(-time.Microsecond)))
		start = transition
	}
}

// The Local location can keep the same name after an OS zone change. Hash
// UTC offsets and transition instants so a restart can detect changed dates
// or clock labels without reading each message.
func archiveCalendarKey(location *time.Location) string {
	h := sha256.New()
	at := time.Date(1969, 12, 31, 0, 0, 0, 0, time.UTC).In(location)
	end := time.Date(10000, 1, 2, 0, 0, 0, 0, time.UTC)
	var entry [12]byte
	for at.Before(end) {
		_, offset := at.Zone()
		binary.LittleEndian.PutUint64(entry[:8], uint64(at.Unix()))
		binary.LittleEndian.PutUint32(entry[8:], uint32(int32(offset)))
		_, _ = h.Write(entry[:])
		_, transition := at.ZoneBounds()
		if !transition.After(at) {
			break
		}
		at = transition
	}
	return hex.EncodeToString(h.Sum(nil))
}

func localArchiveDay(at time.Time) int64 {
	year, month, day := at.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC).UnixMicro()
}

func (a *historyArchive) path(day int64) string {
	if day == archiveBaseline {
		return filepath.Join(a.root, "baseline.log")
	}
	return filepath.Join(a.root, filepath.FromSlash(time.UnixMicro(day).UTC().Format(archiveDateLayout)+".log"))
}

func (a *historyArchive) readDay(ctx context.Context, db *sql.DB, day int64) ([]byte, error) {
	query := "SELECT body FROM messages WHERE " + archiveCandidate("body")
	from, until := day, day+archiveDayMicros
	if day == archiveBaseline {
		query += " AND " + archiveTime("body") + " = 0 ORDER BY rowid"
	} else {
		// Use a bounded index range and filter by the actual local date. This
		// also covers days whose midnight is skipped or repeated.
		if a.location != time.UTC {
			from -= archiveDayMicros
			until += archiveDayMicros
		}
		query += " AND " + archiveTime("body") + " BETWEEN ? AND ? ORDER BY " + archiveTime("body") + ", rowid"
	}
	var args []any
	if day != archiveBaseline {
		args = []any{max(1, from), until - 1}
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var body strings.Builder
	var previous time.Time
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item api.Item
		if json.Unmarshal(raw, &item) != nil || !conversationItem(item, a.exclude) || item.Task != nil || len(item.QuestionCards) > 0 ||
			strings.HasPrefix(item.ID, "worker-question:") ||
			(item.Kind == "assistant" && strings.TrimSpace(item.Text) == api.SilentReminder) {
			continue
		}
		at := time.UnixMicro(item.SeenAt).In(a.location)
		if day != archiveBaseline && localArchiveDay(at) != day {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		if day != archiveBaseline && (previous.IsZero() || at.Sub(previous) >= 15*time.Minute) {
			body.WriteString(at.Format("[15:04]\n"))
		}
		if item.Kind == "user" {
			body.WriteString("user: ")
		}
		body.WriteString(item.Text)
		previous = at
	}
	if body.Len() > 0 {
		body.WriteByte('\n')
	}
	return []byte(body.String()), rows.Err()
}

func publishArchiveDay(path string, body []byte) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if len(body) == 0 {
			return nil
		}
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return errors.New("archive destination is not a regular file")
	case len(body) == 0:
		return deleteHistoryFile(path)
	default:
		old, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(old, body) && info.Mode().Perm()&0222 == 0 {
			return nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".chat-history-*")
	if err != nil {
		return err
	}
	defer removeArchiveFile(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(body); err != nil {
		return err
	}
	if err = tmp.Chmod(0400); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return replaceHistoryFile(tmp.Name(), path)
}

func removeArchiveFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("archive retirement target is not a regular file")
	}
	err = deleteHistoryFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
