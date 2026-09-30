package productrpc

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

type journalEntry struct {
	Digest string `json:"digest"`
	Result Result `json:"result"`
}

type journalDocument struct {
	Version int                     `json:"version"`
	BotID   string                  `json:"botId"`
	Entries map[string]journalEntry `json:"entries"`
}

// Only command digests and receipts are persisted, never prompts, approval
// answers, native credentials, or raw command payloads. Unknown entries survive.
type journal struct {
	mu   sync.Mutex
	path string
	doc  journalDocument
}

func openJournal(path, botID string) (*journal, error) {
	j := &journal{path: path, doc: journalDocument{Version: 1, BotID: botID, Entries: make(map[string]journalEntry)}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > MaxReceipts*4096 || json.Unmarshal(b, &j.doc) != nil || j.doc.Version != 1 || j.doc.BotID != botID || j.doc.Entries == nil || len(j.doc.Entries) > MaxReceipts {
		return nil, errors.New("invalid product command journal")
	}
	for id, entry := range j.doc.Entries {
		if !identifier.MatchString(id) || len(entry.Digest) != 64 || entry.Result.ID != id || (entry.Result.Outcome != "accepted" && entry.Result.Outcome != "rejected" && entry.Result.Outcome != "unknown") {
			return nil, errors.New("invalid product command receipt")
		}
		entry.Result = receiptOnly(entry.Result)
		j.doc.Entries[id] = entry
	}
	return j, nil
}

func (j *journal) reserve(id, digest string) (Result, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e, ok := j.doc.Entries[id]; ok {
		if e.Digest != digest {
			return Result{}, false, errors.New("command id conflict")
		}
		return e.Result, false, nil
	}
	if len(j.doc.Entries) >= MaxReceipts {
		return Result{}, false, errors.New("product receipt limit reached")
	}
	r := Result{ID: id, Outcome: "unknown", Code: "pending"}
	j.doc.Entries[id] = journalEntry{Digest: digest, Result: r}
	if err := localstate.Write(j.path, j.doc); err != nil {
		delete(j.doc.Entries, id)
		return Result{}, false, err
	}
	return r, true, nil
}

func (j *journal) finish(id string, result Result) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	e, ok := j.doc.Entries[id]
	if !ok {
		return errors.New("product command was not reserved")
	}
	old := e
	e.Result = receiptOnly(result)
	j.doc.Entries[id] = e
	if err := localstate.Write(j.path, j.doc); err != nil {
		j.doc.Entries[id] = old
		return err
	}
	return nil
}

func receiptOnly(r Result) Result {
	r.Draft, r.Initialization = nil, nil
	if r.Submission != nil {
		r.Submission = &api.Receipt{ID: r.Submission.ID, Outcome: r.Submission.Outcome}
	}
	return r
}

func (j *journal) lookup(id string) (Result, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	e, ok := j.doc.Entries[id]
	return e.Result, ok
}
