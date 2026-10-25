package websurface

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	_ "modernc.org/sqlite"
)

// Only confirmed terminal ingress receipts can retire. Unknown business receipts
// remain with their original identity, even when the HTTP invocation returned.
func operationTerminal(op operation) bool {
	var result struct {
		Outcome string `json:"outcome"`
		State   string `json:"state"`
	}
	_ = json.Unmarshal(op.Value, &result)
	return op.State == "complete" && !strings.HasPrefix(op.Error, "operation_unconfirmed") && result.Outcome != "unknown" && result.State != "unknown"
}

// SQLite is already used by the product. This table stores compact terminal
// tombstones only, not a second business ledger. IDs are never forgotten, even
// after cookie expiry; a retired ID can never authorize another dispatch.
func (s *Server) archiveDB() (*sql.DB, error) {
	if s.retired != nil {
		return s.retired, nil
	}
	path := filepath.Join(s.config.Directory, "retired-operations.sqlite")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("invalid operation archive")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = f.Chmod(0600)
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS retired (id TEXT PRIMARY KEY, digest TEXT NOT NULL, owner TEXT NOT NULL, error TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '') WITHOUT ROWID`)
	if err == nil {
		err = addArchiveDetail(db)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	s.retired = db
	return db, nil
}

func addArchiveDetail(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(retired)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primary int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primary); err != nil {
			break
		}
		found = found || name == "detail"
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil || found {
		return err
	}
	_, err = db.Exec(`ALTER TABLE retired ADD COLUMN detail TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *Server) lookupOperation(id string) (operation, bool, error) {
	if op, ok := s.operations[id]; ok {
		op, err := s.reconcileOperation(id, op)
		return op, true, err
	}
	db, err := s.archiveDB()
	if err != nil {
		return operation{}, false, err
	}
	op := operation{State: "complete"}
	err = db.QueryRow(`SELECT digest,owner,error,detail FROM retired WHERE id=?`, digest(id)).Scan(&op.Digest, &op.Owner, &op.Error, &op.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return operation{}, false, nil
	}
	return op, err == nil, err
}
func (s *Server) loadOperations() error {
	// Stream the old JSON once; older installations could exceed the old 16 MiB
	// reader cap. The original file is replaced only after durable retirement.
	f, err := os.Open(filepath.Join(s.config.Directory, "operations.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	if err = d.Decode(&s.operations); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return errors.New("invalid operation journal")
	}
	if s.operations == nil {
		s.operations = map[string]operation{}
	}
	// The prior host process cannot complete an admitted authorization invocation.
	// Its grant effect is still unknown; retain the original reference for reads
	// and a separate explicit authorization rather than replaying the write.
	for id, op := range s.operations {
		if (op.BotConnection != nil || op.Remote) && op.State == "pending" {
			op.State = "complete"
			op.Error = "operation_unconfirmed"
			s.operations[id] = op
		}
	}
	return s.compactOperations()
}
func (s *Server) compactOperations() error {
	ids := make([]string, 0)
	size := 0
	for id, op := range s.operations {
		size += len(op.Value)
		if operationTerminal(op) {
			ids = append(ids, id)
		}
	}
	if len(s.operations) < 256 && size < 4<<20 {
		return localstate.WriteConfirmed(filepath.Join(s.config.Directory, "operations.json"), s.operations)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.operations[ids[i]], s.operations[ids[j]]
		if a.At == b.At {
			return ids[i] < ids[j]
		}
		return a.At < b.At
	})
	// Retain recent results for normal response loss, and always retain unknowns.
	retire := len(ids) - 128
	if size >= 4<<20 || len(s.operations) >= 4096 {
		retire = len(ids)
	}
	if retire <= 0 {
		return localstate.WriteConfirmed(filepath.Join(s.config.Directory, "operations.json"), s.operations)
	}
	db, err := s.archiveDB()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids[:retire] {
		op := s.operations[id]
		code := op.Error
		if code == "" {
			code = "operation_retired"
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO retired(id,digest,owner,error,detail) VALUES(?,?,?,?,?)`, digest(id), op.Digest, op.Owner, code, operationDetail(op)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Crash between archive and journal leaves duplicate *terminal* records,
	// never a missing tombstone. Failed replacement keeps original in-memory data.
	next := make(map[string]operation, len(s.operations)-retire)
	for id, op := range s.operations {
		next[id] = op
	}
	for _, id := range ids[:retire] {
		delete(next, id)
	}
	if err = localstate.WriteConfirmed(filepath.Join(s.config.Directory, "operations.json"), next); err != nil {
		return err
	}
	s.operations = next
	return nil
}
