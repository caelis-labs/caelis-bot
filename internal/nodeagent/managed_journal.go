package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type ManagedDisableReceipt struct {
	Request  ManagedDisableRequest `json:"request"`
	Snapshot nodeplane.SnapshotRef `json:"snapshot"`
	Outcome  string                `json:"outcome"`
}
type managedDisableDocument struct {
	Version  int                              `json:"version"`
	NodeID   string                           `json:"nodeId"`
	BotID    string                           `json:"botId"`
	Receipts map[string]ManagedDisableReceipt `json:"receipts"`
}
type ManagedDisableJournal struct {
	mu       sync.Mutex
	path     string
	document managedDisableDocument
}

func canonicalDisableRequest(r ManagedDisableRequest) ManagedDisableRequest {
	r.Lease = nodeplane.Lease{BotID: r.Lease.BotID, NodeID: r.Lease.NodeID, Backend: r.Lease.Backend, Epoch: r.Lease.Epoch}
	return r
}
func OpenManagedDisableJournal(path, nodeID, botID string) (*ManagedDisableJournal, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || CheckPrivateDirectory(filepath.Dir(path)) != nil {
		return nil, errors.New("private managed control journal required")
	}
	d := managedDisableDocument{Version: 1, NodeID: nodeID, BotID: botID, Receipts: map[string]ManagedDisableReceipt{}}
	err := readPrivateJSON(path, &d)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if d.Version != 1 || d.NodeID != nodeID || d.BotID != botID || len(d.Receipts) > 64 {
		return nil, errors.New("managed control journal scope mismatch")
	}
	if d.Receipts == nil {
		d.Receipts = map[string]ManagedDisableReceipt{}
	}
	for id, r := range d.Receipts {
		if id != r.Request.OperationID || !identifier.MatchString(id) || r.Request.Target.NodeID != nodeID || r.Request.Lease.BotID != botID || r.Request != canonicalDisableRequest(r.Request) || (r.Outcome != "accepted" && r.Outcome != "unknown") || r.Outcome == "accepted" && (r.Snapshot.BotID != botID || r.Snapshot.Digest == "") {
			return nil, errors.New("managed control journal invalid")
		}
	}
	return &ManagedDisableJournal{path: path, document: d}, nil
}

// WriteManagedPrivateJSON durably publishes a bounded native intent before any
// effect and fsyncs the containing directory. Existing symlinks are rejected.
func WriteManagedPrivateJSON(path string, value any) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return errors.New("private managed file redirected")
	}
	b, err := json.Marshal(value)
	if err != nil || len(b) > 256<<10 {
		return errors.New("managed journal limit")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".managed-stage-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (j *ManagedDisableJournal) Disabled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.document.Receipts) > 0
}
func (j *ManagedDisableJournal) Lookup(ctx context.Context, r ManagedDisableRequest) (ManagedDisableReceipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r = canonicalDisableRequest(r)
	if err := ctx.Err(); err != nil {
		return ManagedDisableReceipt{}, err
	}
	if r.Target.NodeID != j.document.NodeID || r.Lease.BotID != j.document.BotID || !identifier.MatchString(r.OperationID) {
		return ManagedDisableReceipt{}, errors.New("managed receipt scope mismatch")
	}
	value, ok := j.document.Receipts[r.OperationID]
	if !ok {
		return ManagedDisableReceipt{Request: r, Outcome: "unknown"}, nil
	}
	if value.Request != r {
		return ManagedDisableReceipt{}, errors.New("managed original operation changed")
	}
	return value, nil
}
func (j *ManagedDisableJournal) Begin(ctx context.Context, r ManagedDisableRequest) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r = canonicalDisableRequest(r)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.Target.NodeID != j.document.NodeID || r.Lease.BotID != j.document.BotID || !identifier.MatchString(r.OperationID) || len(j.document.Receipts) >= 64 {
		return errors.New("managed disable intent scope or limit")
	}
	if _, ok := j.document.Receipts[r.OperationID]; ok {
		return errors.New("managed intent already exists")
	}
	next := j.document
	next.Receipts = map[string]ManagedDisableReceipt{}
	for k, v := range j.document.Receipts {
		next.Receipts[k] = v
	}
	next.Receipts[r.OperationID] = ManagedDisableReceipt{Request: r, Outcome: "unknown"}
	if err := WriteManagedPrivateJSON(j.path, next); err != nil {
		return err
	}
	j.document = next
	return nil
}
func (j *ManagedDisableJournal) Finish(ctx context.Context, r ManagedDisableRequest, ref nodeplane.SnapshotRef, outcome string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r = canonicalDisableRequest(r)
	old, ok := j.document.Receipts[r.OperationID]
	if !ok || old.Request != r || (outcome != "accepted" && outcome != "unknown") {
		return errors.New("managed disable completion scope")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if outcome == "accepted" && (ref.BotID != j.document.BotID || ref.Digest == "") {
		return errors.New("managed completion snapshot missing")
	}
	next := j.document
	next.Receipts = map[string]ManagedDisableReceipt{}
	for k, v := range j.document.Receipts {
		next.Receipts[k] = v
	}
	next.Receipts[r.OperationID] = ManagedDisableReceipt{Request: r, Snapshot: ref, Outcome: outcome}
	if err := WriteManagedPrivateJSON(j.path, next); err != nil {
		return err
	}
	j.document = next
	return nil
}
