package caelis

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/statemigration"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/backend/contextseed"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

const currentProjectionVersion = 9

// Hold newly observed native inputs until the prompt's receipt can identify
// them exactly. The canonical transcript remains intact, including on restart.
type pendingInput struct {
	VisibleIDs []string `json:"visibleIDs"`
}

type journal struct {
	StopTarget     *wire.TurnTarget   `json:"stopTarget,omitempty"`
	Work           *workIntent        `json:"work,omitempty"`
	OriginRequests []string           `json:"originRequests,omitempty"`
	OriginOwner    string             `json:"originOwner,omitempty"`
	ReceiptID      string             `json:"receiptID,omitempty"`
	Dream          bool               `json:"dream,omitempty"`
	HostNotice     bool               `json:"hostNotice,omitempty"`
	PendingInput   *pendingInput      `json:"pendingInput,omitempty"`
	TurnID         string             `json:"turnID,omitempty"`
	Scheduled      bool               `json:"scheduled,omitempty"`
	Digest         string             `json:"digest"`
	Path           string             `json:"path"`
	Body           json.RawMessage    `json:"body"`
	Outcome        string             `json:"outcome"`
	Resource       string             `json:"resource,omitempty"`
	Source         wire.SessionSource `json:"source"`

	RenewalDeferred bool `json:"renewalDeferred,omitempty"`
}

// Only old journals derive presentation from their original native source.
// New notices use ordinary evidence input and an independent local marker.
func (j journal) hostNotice() bool {
	return j.HostNotice || j.Source.Kind == "application_summary"
}

// A deferred empty-session create has no prompt or effect in the source
// conversation. Keep reconciling its original receipt without freezing chat.
func (j journal) deferredRenewal(id string) bool {
	return j.RenewalDeferred && j.Path == "/sessions" && strings.HasPrefix(id, "tool-renew-")
}

type view struct {
	// Last canonical main target survives idle bootstrap states that omit it.
	// This is observation evidence only, never authority for a new cancellation.
	HeadTarget      wire.TurnTarget                  `json:"headTarget,omitempty"`
	WorkerOutput    *workerTranscript                `json:"workerOutput,omitempty"`
	InputScopes     map[string]inputScope            `json:"inputScopes,omitempty"`
	Usage           api.ContextUsage                 `json:"-"`
	UsageTurn       string                           `json:"-"`
	ModelTurn       string                           `json:"-"`
	UsageEvidence   string                           `json:"-"`
	Reviews         map[string]reviewFact            `json:"reviews,omitempty"`
	LiveReviews     map[string]reviewFact            `json:"-"`
	CommandCaughtUp bool                             `json:"-"`
	CommandResults  map[string]commandResultEvidence `json:"commandResults,omitempty"`
	ApprovalDirty   bool                             `json:"approvalDirty,omitempty"`
	ApprovalVersion uint64                           `json:"-"`
	Turns           map[string]string                `json:"turns,omitempty"`
	RetiredTurns    map[string]bool                  `json:"retiredTurns,omitempty"`
	Observed        uint64                           `json:"-"`
	State           wire.SessionState                `json:"state"`
	Items           []api.Item                       `json:"items"`
	Cursor          string                           `json:"cursor"`
	HistoryBefore   string                           `json:"historyBefore,omitempty"`
	Seen            map[string]bool                  `json:"seen"`
	Failure         string                           `json:"failure,omitempty"`
}
type typedRecord struct {
	ReceiptID string          `json:"receiptID,omitempty"`
	Path      string          `json:"path"`
	Digest    string          `json:"digest,omitempty"`
	Body      json.RawMessage `json:"body"`
	Result    json.RawMessage `json:"result,omitempty"`
	Outcome   string          `json:"outcome"`
}
type callRecord struct {
	Call    wire.SessionToolCall        `json:"call"`
	Phase   string                      `json:"phase"`
	Receipt *wire.SessionToolCallResult `json:"receipt,omitempty"`
}
type binding struct {
	migrationSource   json.RawMessage
	BackgroundResults map[string]api.BackgroundResult `json:"backgroundResults,omitempty"`

	// Keep callback formats across a crash or a disabled experiment so old
	// pending calls can receive an unknown receipt without redispatching effects.
	ContentCatalogs    map[string]map[string]bool           `json:"contentCatalogs,omitempty"`
	Context            contextseed.State                    `json:"context,omitempty"`
	ContextInputs      map[string]int                       `json:"contextInputs,omitempty"`
	ContextRequests    map[string]string                    `json:"contextRequests,omitempty"`
	PastSessions       []string                             `json:"pastSessions,omitempty"`
	RenewedBy          string                               `json:"renewedBy,omitempty"`
	CommandFollowups   map[string]commandFollowup           `json:"commandFollowups,omitempty"`
	Version            int                                  `json:"version"`
	ProjectionVersion  int                                  `json:"projectionVersion"`
	StoreID            string                               `json:"storeID"`
	StoreDirectory     string                               `json:"storeDirectory,omitempty"`
	Endpoint           string                               `json:"endpoint"`
	InstanceID         string                               `json:"instanceID"`
	PrincipalID        string                               `json:"principalID"`
	Session            wire.SessionBinding                  `json:"session"`
	CreateID           string                               `json:"createID"`
	Operations         map[string]journal                   `json:"operations"`
	Views              map[string]*view                     `json:"views"`
	Calls              map[string]callRecord                `json:"calls"`
	Typed              map[string]typedRecord               `json:"typed"`
	Configurations     map[string]wire.SessionConfiguration `json:"configurations"`
	WorkSourceIdentity string                               `json:"workSourceIdentity,omitempty"`
	Workers            map[string]worker                    `json:"workers"`
	Grants             map[string]grantRecord               `json:"grants"`
	FinishedTurn       string                               `json:"finishedTurn"`
	LastReceipt        api.Receipt                          `json:"lastReceipt"`
}

// privateRead refuses redirected/world-readable metadata and bounds allocations.
func privateRead(path string, limit int64) ([]byte, error) {
	before, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() || !privateState(path, before) {
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
	b := binding{Version: 2, Operations: map[string]journal{}, Views: map[string]*view{}, Calls: map[string]callRecord{}}
	b.Typed = map[string]typedRecord{}
	b.Configurations = map[string]wire.SessionConfiguration{}
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
	upgraded, migrated, e := statemigration.Upgrade(raw)
	if e != nil {
		return b, e
	}
	if migrated {
		b.migrationSource = append(json.RawMessage(nil), raw...)
	}
	if json.Unmarshal(upgraded, &b) != nil || b.Version != 2 || b.Operations == nil || b.Views == nil || b.Calls == nil {
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
			v.Reviews, v.LiveReviews = nil, nil
			v.Items = []api.Item{}
			v.Cursor = ""
			v.HistoryBefore = ""
			v.Seen = map[string]bool{}
			v.Failure = ""
			v.Turns = nil
			v.CommandResults = nil
		}
		for id, f := range b.CommandFollowups {
			op := "command-result-" + digest([]byte(b.Session.SessionId+"\x00"+id))
			if j, ok := b.Operations[op]; ok && (j.Outcome == "rejected" || j.Outcome == "conflicted") {
				f.Done = false
				f.DeliveryFailure = j.Outcome
				b.CommandFollowups[id] = f
			}
		}
		b.ProjectionVersion = currentProjectionVersion
	}
	return b, nil
}
func (s *Session) saveLocked() error {
	if s.loadErr != nil {
		return s.loadErr
	}
	if len(s.state.migrationSource) > 0 {
		archive := s.path + ".v1-retired.json"
		existing, err := privateRead(archive, 64<<20)
		if errors.Is(err, os.ErrNotExist) {
			err = privateWrite(archive, s.state.migrationSource)
		} else if err == nil {
			var a, b bytes.Buffer
			if json.Compact(&a, existing) != nil || json.Compact(&b, s.state.migrationSource) != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
				err = errors.New("Caelis 旧连接归档与迁移来源不一致，请保留文件")
			}
		}
		if err != nil {
			return err
		}
	}
	s.captureBackgroundResultsLocked()
	if e := privateWrite(s.path, s.state); e != nil {
		s.issue = "Caelis 连接记录暂未保存；原请求已保留，将自动重试"
		return e
	}
	s.state.migrationSource = nil
	if s.issue == "Caelis 连接记录暂未保存；原请求已保留，将自动重试" {
		s.issue = ""
	}
	return nil
}
func (s *Session) bumpLocked() {
	s.revision++
	out := s.snapshotLocked()
	s.cached.Store(&out)
	recovery := s.recoveryLocked()
	s.cachedRecovery.Store(&recovery)
	close(s.changed)
	s.changed = make(chan struct{})
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func value[T any](p *T) (z T) {
	if p != nil {
		return *p
	}
	return z
}
func pointer[T any](v T) *T { return &v }

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
	if !info.IsDir() || !privateState(path, info) {
		return errors.New("私有连接目录权限不安全")
	}
	return nil
}
func privateWrite(path string, v any) error {
	dir := filepath.Dir(path)
	if e := localstate.EnsurePrivateDirectory(dir); e != nil {
		return e
	}
	if e := privateDir(dir); e != nil {
		return e
	}
	if e := localstate.Write(path, v); e != nil {
		return e
	}
	// localstate.Write includes the parent-directory durability barrier.
	return nil
}
