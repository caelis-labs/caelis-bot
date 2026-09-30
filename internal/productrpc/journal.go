package productrpc

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type journalEntry struct {
	Digest string `json:"digest"`
	Result Result `json:"result"`
	// Only installation selectors needed to reconcile the original native intent.
	Runtime *productmanagement.RuntimeCommand `json:"runtime,omitempty"`
	// Only the category is needed to fence unresolved settings after restart.
	Execution bool `json:"execution,omitempty"`
}

type journalDocument struct {
	Version int                     `json:"version"`
	BotID   string                  `json:"botId"`
	Entries map[string]journalEntry `json:"entries"`
}

// Only command digests and receipts are persisted, never prompts, approval
// answers, native credentials, or raw command payloads. Unknown entries survive.
type journal struct {
	mu          sync.Mutex
	path        string
	doc         journalDocument
	write       func(string, journalDocument) error
	unavailable bool
}

func openJournal(path, botID string) (*journal, error) {
	j := &journal{path: path, doc: journalDocument{Version: 1, BotID: botID, Entries: make(map[string]journalEntry)}, write: durableJournalWrite}
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
		if entry.Runtime != nil && (entry.Runtime.ID != id || entry.Runtime.BotID != botID || !identifier.MatchString(entry.Runtime.Generation) || !validRuntimeManagement(*entry.Runtime, false)) {
			return nil, errors.New("invalid original installation intent")
		}
		if entry.Execution {
			v := entry.Result.Execution
			if v == nil || v.ID != id || v.BotID != botID || !identifier.MatchString(v.Generation) || v.Outcome != entry.Result.Outcome {
				return nil, errors.New("invalid execution receipt")
			}
			if entry.Result.Outcome == "unknown" && entry.Result.Code == "pending" {
				entry.Result.Code = "execution-outcome-unresolved"
				copy := *v
				copy.Code = entry.Result.Code
				entry.Result.Execution = &copy
			}
		}
		entry.Result = receiptOnly(entry.Result)
		j.doc.Entries[id] = entry
	}
	return j, nil
}

func (j *journal) reserve(id, digest string, runtime ...*productmanagement.RuntimeCommand) (Result, bool, error) {
	var original *productmanagement.RuntimeCommand
	if len(runtime) == 1 {
		original = runtime[0]
	}
	return j.reserveCommand(id, digest, original, nil)
}

func (j *journal) reserveCommand(id, digest string, runtime *productmanagement.RuntimeCommand, execution *productmanagement.ExecutionCommand) (Result, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e, ok := j.doc.Entries[id]; ok {
		if e.Digest != digest {
			return Result{}, false, errors.New("command id conflict")
		}
		return e.Result, false, nil
	}
	if j.unavailable {
		return Result{}, false, errors.New("receipt durability unavailable")
	}
	if len(j.doc.Entries) >= MaxReceipts {
		return Result{}, false, errors.New("product receipt limit reached")
	}
	r := Result{ID: id, Outcome: "unknown", Code: "pending"}
	entry := journalEntry{Digest: digest, Result: r}
	if runtime != nil {
		original := *runtime
		entry.Runtime = &original
		r.RuntimeManagement = &productmanagement.RuntimeResult{Scope: original.Scope, ID: id, Outcome: "unknown", Code: "pending"}
		entry.Result = r
	}
	if execution != nil {
		entry.Execution = true
		r.Execution = &productmanagement.ExecutionResult{Scope: execution.Scope, ID: id, Outcome: "unknown", Code: "pending"}
		entry.Result = r
	}
	j.doc.Entries[id] = entry
	if err := j.write(j.path, j.doc); err != nil {
		// Publication may have reached rename before directory sync failed.
		// Keep the unknown marker and fence fresh admission in this owner.
		j.unavailable = true
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
	if e.Execution && result.Execution == nil {
		v := *e.Result.Execution
		v.Outcome, v.Code = result.Outcome, result.Code
		result.Execution = &v
	}
	e.Result = receiptOnly(result)
	j.doc.Entries[id] = e
	if err := j.write(j.path, j.doc); err != nil {
		j.doc.Entries[id] = old
		j.unavailable = true
		return err
	}
	return nil
}

// Native dispatch requires the reservation's directory entry to be durable as
// well as its file bytes. A rename-only writer is insufficient after power loss.
func durableJournalWrite(path string, doc journalDocument) error {
	dir := filepath.Dir(path)
	if err := durableDirectory(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(doc)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func durableDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return errors.New("receipt parent is not a directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if err = durableDirectory(parent); err != nil {
		return err
	}
	if err = os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return syncDirectory(parent)
}

func receiptOnly(r Result) Result {
	r.Draft, r.Initialization = nil, nil
	if r.Configuration != nil {
		v := *r.Configuration
		v.Native.OperationID, v.Native.Message = "", ""
		r.Configuration = &v
	}
	if r.RuntimeManagement != nil {
		v := *r.RuntimeManagement
		v.Status.Message = ""
		r.RuntimeManagement = &v
	}
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

// Called under the service mutation lock. Pending requests from this owner are
// queued; reopened pending entries were converted to unresolved in openJournal.
func (j *journal) executionUnresolved(except string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.unavailable {
		return true
	}
	for id, e := range j.doc.Entries {
		if id != except && e.Execution && e.Result.Outcome == "unknown" && e.Result.Code != "pending" {
			return true
		}
	}
	return false
}
