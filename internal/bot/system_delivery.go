package bot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Delivery contains no task, permission or event state. Retained identities are
// needed even after a producer is read/retired or the selected Runtime changes.
// Bodies stay at their source/native intent; this log stores only delivery facts.
type systemDeliveryRecord struct {
	Source      api.SystemSource `json:"source,omitempty"`
	Provider    string           `json:"provider,omitempty"`
	Fingerprint string           `json:"fingerprint,omitempty"`
	Outcome     string           `json:"outcome"`
}
type systemDeliveryLog struct {
	mu    sync.Mutex
	path  string
	state systemDeliveryState
}
type systemDeliveryState struct {
	Version              int                             `json:"version"`
	InteractionsMigrated bool                            `json:"interactionsMigrated,omitempty"`
	Records              map[string]systemDeliveryRecord `json:"records"`
}

func openSystemDeliveries(root string) (*systemDeliveryLog, error) {
	d := &systemDeliveryLog{path: filepath.Join(root, "system-deliveries.json"), state: systemDeliveryState{Version: 1, Records: map[string]systemDeliveryRecord{}}}
	if b, err := os.ReadFile(d.path); err == nil {
		if json.Unmarshal(b, &d.state) != nil || d.state.Version != 1 || d.state.Records == nil {
			return nil, errors.New("invalid system delivery log")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for id, record := range d.state.Records {
		if id == "" || (record.Outcome != "accepted" && record.Outcome != "rejected" && record.Outcome != "unknown") {
			return nil, errors.New("invalid system delivery receipt")
		}
	}
	if !d.state.InteractionsMigrated {
		// Preserve old accepted/uncertain identities, without guessing their
		// Runtime or replaying an operation whose original owner was not recorded.
		b, err := os.ReadFile(filepath.Join(root, "worker-interaction-notices.json"))
		if err == nil {
			var old struct {
				Version int
				States  map[string]string
			}
			if json.Unmarshal(b, &old) != nil || old.Version != 1 || old.States == nil {
				return nil, errors.New("invalid legacy interaction notice log")
			}
			for key, outcome := range old.States {
				sum := sha256.Sum256([]byte(key))
				id := fmt.Sprintf("worker-interaction-%x", sum[:])
				if _, exists := d.state.Records[id]; exists {
					continue
				}
				if outcome != "accepted" {
					outcome = "unknown"
				}
				d.state.Records[id] = systemDeliveryRecord{Outcome: outcome}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		d.state.InteractionsMigrated = true
		if err := localstate.Write(d.path, d.state); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *systemDeliveryLog) record(id string, next systemDeliveryRecord) error {
	previous, exists := d.state.Records[id]
	d.state.Records[id] = next
	if err := localstate.Write(d.path, d.state); err != nil {
		if exists {
			d.state.Records[id] = previous
		} else {
			delete(d.state.Records, id)
		}
		return err
	}
	return nil
}

// ImportSystemReceipts migrates old producer delivery proofs once. Unknown is
// a no-replay fence, never evidence that an input was accepted.
func (r *Runtime) ImportSystemReceipts(receipts []api.Receipt) error {
	if r.deliveryErr != nil {
		return r.deliveryErr
	}
	d := r.deliveries
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, receipt := range receipts {
		if receipt.ID == "" {
			continue
		}
		if _, exists := d.state.Records[receipt.ID]; exists {
			continue
		}
		outcome := receipt.Outcome
		if outcome != "accepted" {
			outcome = "unknown"
		}
		if err := d.record(receipt.ID, systemDeliveryRecord{Outcome: outcome}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) SystemReceipt(id string) api.Receipt {
	unknown := api.Receipt{ID: id, Outcome: "unknown"}
	if r.deliveries == nil {
		return unknown
	}
	r.mu.Lock()
	engine, provider := r.engine, r.provider
	r.mu.Unlock()
	d := r.deliveries
	d.mu.Lock()
	old, exists := d.state.Records[id]
	d.mu.Unlock()
	if exists && old.Outcome != "unknown" {
		return api.Receipt{ID: id, Outcome: old.Outcome}
	}
	if old.Provider != "" && old.Provider != provider {
		return unknown
	}
	native, ok := engine.(api.SystemDelivery)
	if !ok {
		return unknown
	}
	receipt := native.SystemReceipt(id)
	if receipt.ID != id || receipt.Deferred || (receipt.Outcome != "accepted" && receipt.Outcome != "rejected") {
		return unknown
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	// Recheck after the native read; a concurrent terminal proof wins.
	current, exists := d.state.Records[id]
	if exists && current.Outcome != "unknown" {
		return api.Receipt{ID: id, Outcome: current.Outcome}
	}
	current.Outcome = receipt.Outcome
	if d.record(id, current) != nil {
		return unknown
	}
	return receipt
}

// SubmitSystem is the only host entry for unsolicited resident input. Producers
// call it without their native adapter locks. Tick uses the locked variant.
func (r *Runtime) SubmitSystem(ctx context.Context, message api.SystemMessage) (api.Receipt, error) {
	r.step.Lock()
	defer r.step.Unlock()
	return r.submitSystemLocked(ctx, message)
}

func (r *Runtime) submitSystemLocked(ctx context.Context, message api.SystemMessage) (api.Receipt, error) {
	rejected := api.Receipt{ID: message.ID, Outcome: "rejected"}
	if !message.Valid() {
		return rejected, errors.New("invalid system message")
	}
	if r.deliveryErr != nil {
		return rejected, r.deliveryErr
	}
	b, _ := json.Marshal(message)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(b))
	r.deliveries.mu.Lock()
	old := r.deliveries.state.Records[message.ID]
	r.deliveries.mu.Unlock()
	if old.Fingerprint != "" && old.Fingerprint != fingerprint {
		return rejected, errors.New("system message identity reused with different content")
	}
	// Reconciliation precedes admission: a busy/new context is not permission
	// to dispatch an uncertain original operation again.
	receipt := r.SystemReceipt(message.ID)
	d := r.deliveries
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.state.Records[message.ID]; exists {
		return receipt, nil
	}
	rejected.Deferred = true
	if ctx.Err() != nil {
		return rejected, ctx.Err()
	}
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped || r.paused || r.engine == nil || r.userWaiting.Load() > 0 {
		return rejected, nil
	}
	if r.initialization != nil && r.initialization.Initialization().Status != "accepted" {
		return rejected, nil
	}
	if r.handoff != nil {
		if pending := r.handoff.pending(); pending.CallID != "" && pending.NewSession == "" {
			return rejected, nil
		}
	}
	if !r.engine.Snapshot().CanSend {
		return rejected, nil
	}
	native, ok := r.engine.(api.SystemDelivery)
	if !ok {
		return rejected, errors.New("runtime has no system input adapter")
	}
	original := systemDeliveryRecord{Source: message.Source, Provider: r.provider, Fingerprint: fingerprint, Outcome: "unknown"}
	if err := d.record(message.ID, original); err != nil {
		return rejected, err
	}
	// Do not hold the log lock during native I/O or snapshot callbacks.
	d.mu.Unlock()
	receipt, err := native.SubmitSystem(ctx, message)
	d.mu.Lock()
	// A native event can settle the original receipt while dispatch unwinds.
	// Its terminal proof must survive a late transport-unknown return.
	if settled := d.state.Records[message.ID]; settled.Outcome != "unknown" {
		return api.Receipt{ID: message.ID, Outcome: settled.Outcome}, nil
	}
	if receipt.ID != message.ID || (receipt.Outcome != "accepted" && receipt.Outcome != "rejected" && receipt.Outcome != "unknown") {
		return api.Receipt{ID: message.ID, Outcome: "unknown"}, errors.Join(err, errors.New("system receipt identity or outcome mismatch"))
	}
	if receipt.Outcome == "rejected" && receipt.Deferred {
		// The adapter attests that no native operation existed. A later eligible
		// attempt is safe; unknown and definitive rejections retain their identity.
		delete(d.state.Records, message.ID)
		if save := localstate.Write(d.path, d.state); save != nil {
			d.state.Records[message.ID] = original
			return api.Receipt{ID: message.ID, Outcome: "unknown"}, errors.Join(err, save)
		}
		return receipt, err
	}
	original.Outcome = receipt.Outcome
	if save := d.record(message.ID, original); save != nil {
		return api.Receipt{ID: message.ID, Outcome: "unknown"}, errors.Join(err, save)
	}
	return receipt, err
}
