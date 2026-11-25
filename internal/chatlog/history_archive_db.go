package chatlog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// The partial expression index orders the same way as the original projection:
// SeenAt followed by SQLite insertion order. Missing times sort by rowid.
func archiveTime(body string) string {
	return "CASE WHEN json_valid(" + body + ") THEN max(COALESCE(CAST(json_extract(" + body + ",'$.seenAt') AS INTEGER),0),0) ELSE 0 END"
}

func archiveCandidate(body string) string {
	return fmt.Sprintf(`CASE WHEN json_valid(%[1]s) THEN
		json_extract(%[1]s,'$.kind') IN ('user','assistant')
		AND json_extract(%[1]s,'$.status') IN ('accepted','completed')
		AND json_extract(%[1]s,'$.text') <> ''
		ELSE 0 END`, body)
}

func markArchiveChanges(times string) string {
	return fmt.Sprintf(`INSERT INTO chat_archive_dirty(bucket, first_seen, last_seen)
		SELECT CASE WHEN seen=0 THEN %d ELSE seen/%d*%d END, min(seen), max(seen)
		FROM (%s) WHERE true GROUP BY 1
		ON CONFLICT(bucket) DO UPDATE SET revision=revision+1,
			first_seen=min(first_seen,excluded.first_seen), last_seen=max(last_seen,excluded.last_seen);`,
		archiveBaseline, archiveDayMicros, archiveDayMicros, times)
}

func (a *historyArchive) prepare(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema
		WHERE type='table' AND name='chat_archive_dirty')`).Scan(&ready)
	if err != nil || (ready && a.repair != nil) {
		return err
	}
	if ready {
		var calendar string
		err = db.QueryRowContext(ctx, "SELECT calendar FROM chat_archive_calendar").Scan(&calendar)
		if err != nil || calendar == a.calendar {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !ready {
		if err = createArchiveSchema(ctx, tx); err != nil {
			return err
		}
	}
	// First import and calendar changes are the only full scans. Older output
	// days retain their previous calendar key until rechecked successfully.
	if _, err = tx.ExecContext(ctx, markArchiveChanges("SELECT "+archiveTime("body")+" AS seen FROM messages WHERE "+archiveCandidate("body"))); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chat_archive_calendar(id,calendar) VALUES(1,?)
		ON CONFLICT(id) DO UPDATE SET calendar=excluded.calendar`, a.calendar); err != nil {
		return err
	}
	return tx.Commit()
}

func createArchiveSchema(ctx context.Context, tx *sql.Tx) error {
	selectTime := func(body string) string {
		return "SELECT " + archiveTime(body) + " AS seen WHERE " + archiveCandidate(body)
	}
	// Changes are queued with the message transaction. Both dates are marked
	// when a row moves, and in-progress text never queues archive work.
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE chat_archive_calendar (id INTEGER PRIMARY KEY CHECK(id=1), calendar TEXT NOT NULL);
		CREATE TABLE chat_archive_dirty (
			bucket INTEGER PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 1,
			first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL
		);
		CREATE TABLE chat_archive_files (day INTEGER PRIMARY KEY, calendar TEXT NOT NULL);
		CREATE INDEX chat_archive_time ON messages(`+archiveTime("body")+`) WHERE `+archiveCandidate("body")+`;
		CREATE TRIGGER chat_archive_insert AFTER INSERT ON messages BEGIN
			`+markArchiveChanges(selectTime("NEW.body"))+`
		END;
		CREATE TRIGGER chat_archive_update AFTER UPDATE OF body ON messages WHEN OLD.body IS NOT NEW.body BEGIN
			`+markArchiveChanges(selectTime("OLD.body")+" UNION "+selectTime("NEW.body"))+`
		END;
		CREATE TRIGGER chat_archive_delete AFTER DELETE ON messages BEGIN
			`+markArchiveChanges(selectTime("OLD.body"))+`
		END;`)
	return err
}

type archiveBucket struct {
	start    int64
	revision int64
	first    int64
	last     int64
}

func pendingArchiveBuckets(ctx context.Context, db *sql.DB) ([]archiveBucket, error) {
	rows, err := db.QueryContext(ctx, "SELECT bucket, revision, first_seen, last_seen FROM chat_archive_dirty")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []archiveBucket
	for rows.Next() {
		var bucket archiveBucket
		if err = rows.Scan(&bucket.start, &bucket.revision, &bucket.first, &bucket.last); err != nil {
			return nil, err
		}
		pending = append(pending, bucket)
	}
	return pending, rows.Err()
}

func (a *historyArchive) missingDays(ctx context.Context, db *sql.DB) (map[int64]struct{}, error) {
	rows, err := db.QueryContext(ctx, "SELECT day, calendar FROM chat_archive_files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[int64]string)
	for rows.Next() {
		var day int64
		var calendar string
		if err = rows.Scan(&day, &calendar); err != nil {
			return nil, err
		}
		known[day] = calendar
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Finish the SQLite read before filesystem checks so a slow path cannot
	// keep the IM writer waiting on this inventory.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	missing := make(map[int64]struct{})
	for day, calendar := range known {
		info, err := os.Lstat(a.path(day))
		if calendar != a.calendar || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			missing[day] = struct{}{}
		}
	}
	return missing, nil
}

func (a *historyArchive) acknowledge(ctx context.Context, db *sql.DB, pending []archiveBucket, published map[int64]bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for day, exists := range published {
		if exists {
			_, err = tx.ExecContext(ctx, `INSERT INTO chat_archive_files(day,calendar) VALUES(?,?)
				ON CONFLICT(day) DO UPDATE SET calendar=excluded.calendar WHERE calendar<>excluded.calendar`, day, a.calendar)
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM chat_archive_files WHERE day=?", day)
		}
		if err != nil {
			return err
		}
	}
	for _, bucket := range pending {
		complete := true
		for _, day := range a.bucketDays(bucket) {
			if _, ok := published[day]; !ok {
				complete = false
				break
			}
		}
		if complete {
			// A concurrent commit after our reads must still be projected.
			if _, err = tx.ExecContext(ctx, "DELETE FROM chat_archive_dirty WHERE bucket=? AND revision=?", bucket.start, bucket.revision); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
