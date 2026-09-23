package caelis

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const currentProjectionVersion = 4

type journal struct {
	Digest   string                 `json:"digest"`
	Path     string                 `json:"path"`
	Body     json.RawMessage        `json:"body"`
	Outcome  string                 `json:"outcome"`
	Resource string                 `json:"resource,omitempty"`
	Source   wire.ApplicationSource `json:"source"`
}
type view struct {
	Observed uint64            `json:"-"`
	State    wire.SessionState `json:"state"`
	Items    []api.Item        `json:"items"`
	Cursor   string            `json:"cursor"`
	Seen     map[string]bool   `json:"seen"`
	Failure  string            `json:"failure,omitempty"`
}
type typedRecord struct {
	Path    string          `json:"path"`
	Digest  string          `json:"digest,omitempty"`
	Body    json.RawMessage `json:"body"`
	Result  json.RawMessage `json:"result,omitempty"`
	Outcome string          `json:"outcome"`
}
type callRecord struct {
	Call    wire.ApplicationCall        `json:"call"`
	Phase   string                      `json:"phase"`
	Receipt *wire.ApplicationCallResult `json:"receipt,omitempty"`
}
type binding struct {
	Version           int                                      `json:"version"`
	ProjectionVersion int                                      `json:"projectionVersion"`
	StoreID           string                                   `json:"storeID"`
	Endpoint          string                                   `json:"endpoint"`
	InstanceID        string                                   `json:"instanceID"`
	PrincipalID       string                                   `json:"principalID"`
	Connection        wire.ApplicationConnection               `json:"connection"`
	Session           wire.ApplicationBinding                  `json:"session"`
	CreateID          string                                   `json:"createID"`
	Operations        map[string]journal                       `json:"operations"`
	Views             map[string]*view                         `json:"views"`
	Calls             map[string]callRecord                    `json:"calls"`
	Typed             map[string]typedRecord                   `json:"typed"`
	Configurations    map[string]wire.ApplicationConfiguration `json:"configurations"`
	Workers           map[string]worker                        `json:"workers"`
	Grants            map[string]grantRecord                   `json:"grants"`
	FinishedTurn      string                                   `json:"finishedTurn"`
	LastReceipt       api.Receipt                              `json:"lastReceipt"`
}

// privateRead refuses redirected/world-readable metadata and bounds allocations.
func privateRead(path string, limit int64) ([]byte, error) {
	before, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || !owned(before) {
		return nil, errors.New("私有连接文件权限不安全")
	}
	if e := privateDir(filepath.Dir(path)); e != nil {
		return nil, e
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		return nil, errors.New("连接文件已改变")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("连接文件读取失败")
	}
	return b, nil
}
func loadBinding(path string) (binding, error) {
	b := binding{Version: 1, Operations: map[string]journal{}, Views: map[string]*view{}, Calls: map[string]callRecord{}}
	b.Typed = map[string]typedRecord{}
	b.Configurations = map[string]wire.ApplicationConfiguration{}
	b.Workers = map[string]worker{}
	b.Grants = map[string]grantRecord{}
	raw, e := privateRead(path, 64<<20)
	if errors.Is(e, os.ErrNotExist) {
		b.ProjectionVersion = currentProjectionVersion
		return b, nil
	}
	if e != nil {
		return b, e
	}
	if json.Unmarshal(raw, &b) != nil || b.Version != 1 || b.Operations == nil || b.Views == nil || b.Calls == nil {
		return b, errors.New("Caelis 连接记录无法识别，请保留文件")
	}
	if b.ProjectionVersion > currentProjectionVersion || b.ProjectionVersion < 0 {
		return b, errors.New("Caelis 消息投影版本无法识别，请保留文件")
	}
	if b.ProjectionVersion < currentProjectionVersion {
		// Rebuild only the derived transcript with the current projection.
		// Native identity, command journals and desktop receipts remain intact.
		for _, v := range b.Views {
			if v == nil {
				continue
			}
			v.Items = []api.Item{}
			v.Cursor = ""
			v.Seen = map[string]bool{}
			v.Failure = ""
		}
		b.ProjectionVersion = currentProjectionVersion
	}
	return b, nil
}
func (s *Session) saveLocked() error {
	if e := privateWrite(s.path, s.state); e != nil {
		s.issue = "Caelis 连接记录未能保存，已暂停发送"
		s.connected = false
		return e
	}
	return nil
}
func (s *Session) bumpLocked() { s.revision++; close(s.changed); s.changed = make(chan struct{}) }
func clone[T any](v T) T       { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func value[T any](p *T) (z T) {
	if p != nil {
		return *p
	}
	return z
}
func pointer[T any](v T) *T { return &v }
func secretPath(path string) string {
	return filepath.Join(filepath.Dir(path), "application-credential.json")
}

func succeeded(v wire.Outcome) bool { return v == wire.OutcomeAccepted || v == wire.OutcomeCommitted }
func productOutcome(v wire.Outcome) string {
	if succeeded(v) {
		return "accepted"
	}
	if v == wire.OutcomeRejected || v == wire.OutcomeConflicted {
		return "rejected"
	}
	return "unknown"
}

func privateDir(path string) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !owned(info) {
		return errors.New("私有连接目录权限不安全")
	}
	return nil
}
func privateWrite(path string, v any) error {
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := privateDir(dir); e != nil {
		return e
	}
	if e := localstate.Write(path, v); e != nil {
		return e
	}
	// Flush the renamed journal entry before a native side effect is permitted.
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
