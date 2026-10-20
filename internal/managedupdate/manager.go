package managedupdate

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type transaction struct {
	ID, Offer, Previous, Phase, When string
}
type record struct {
	Digest  string  `json:"digest"`
	Receipt Receipt `json:"receipt"`
}
type journal struct {
	Status      Status            `json:"status"`
	Offer       *Offer            `json:"offer,omitempty"`
	Transaction *transaction      `json:"transaction,omitempty"`
	Receipts    map[string]record `json:"receipts"`
	LastCheck   int64             `json:"lastCheck"`
}
type Manager struct {
	mu             sync.Mutex
	root, platform string
	trust          Trust
	state          journal
	ctx            context.Context
	cancel         context.CancelFunc
	work           sync.WaitGroup
	busy           bool
	boot           Boot
	ready          bool
}

func Open(ctx context.Context, root, platform string, t Trust) (*Manager, error) {
	if e := privateDirectory(root); e != nil {
		return nil, e
	}
	var e error
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{root: root, platform: platform, trust: t, ctx: ctx, cancel: cancel}
	path := filepath.Join(root, "update.json")
	if _, e = os.Lstat(path); e == nil {
		if e = readJSON(path, &m.state); e != nil {
			cancel()
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		cancel()
		return nil, e
	}
	if m.state.Receipts == nil {
		m.state.Receipts = map[string]record{}
	}
	m.state.Status.Protocol = Protocol
	m.state.Status.Mode = "managed"
	m.state.Status.TestArtifact = t.QA
	m.state.Status.BootID = ""
	if m.state.Status.State == "" {
		m.state.Status.State = "idle"
		m.state.Status.Automatic = true
	}
	if m.state.Offer != nil {
		valid, e := Verify(m.state.Offer.Bytes, m.state.Offer.Signature, t.Key, platform, t.QA)
		if e != nil || valid.ID != m.state.Offer.ID {
			cancel()
			return nil, problem("stored_offer_invalid")
		}
		m.state.Offer = &valid
	}
	if m.state.Status.State == "checking" || m.state.Status.State == "downloading" {
		m.state.Status.State = "idle"
		if m.state.Offer != nil {
			m.state.Status.State = "available"
		}
		m.state.Status.Reason = "download_interrupted"
	}
	if e = m.recoverSelection(); e != nil {
		cancel()
		return nil, e
	}
	// Recover the durable transaction before reading selected executable bytes:
	// an unacknowledged candidate may itself be the corrupted/unhealthy package.
	id, e := selected(root)
	if e != nil {
		cancel()
		return nil, e
	}
	o, e := loadOffer(root, id, t, platform)
	if e != nil {
		cancel()
		return nil, e
	}
	m.state.Status.Current = o.Manifest.Identity()
	if e = m.prune(); e != nil {
		cancel()
		return nil, e
	}
	if e = m.save(); e != nil {
		cancel()
		return nil, e
	}
	return m, nil
}

func (m *Manager) save() error {
	m.state.Status.Revision++
	if e := writeJSON(filepath.Join(m.root, "update.json"), m.state); e != nil {
		m.state.Status.State = "unknown"
		m.state.Status.Reason = "persistence_unconfirmed"
		return e
	}
	return nil
}
func (m *Manager) Close()         { m.cancel(); m.work.Wait() }
func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Status }
func (m *Manager) Receipt(id string) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.state.Receipts[id]; ok {
		if r.Receipt.State == "unknown" {
			return Receipt{}, problem("receipt_unconfirmed")
		}
		return r.Receipt, nil
	}
	return Receipt{}, problem("receipt_not_found")
}
func (m *Manager) Command(c Command) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !ingressID.MatchString(c.ID) {
		return Receipt{}, problem("request_id_required")
	}
	raw, _ := json.Marshal(c)
	fingerprint := hash(raw)
	if old, ok := m.state.Receipts[c.ID]; ok {
		if old.Digest != fingerprint {
			return Receipt{}, problem("request_conflict")
		}
		if old.Receipt.State == "unknown" {
			return Receipt{}, problem("receipt_unconfirmed")
		}
		return old.Receipt, nil
	}
	if len(m.state.Receipts) >= 4096 {
		return Receipt{}, problem("receipt_capacity")
	}
	if m.state.Status.State == "unknown" {
		return Receipt{}, problem("update_unconfirmed")
	}
	r := Receipt{ID: c.ID, State: "accepted"}
	start := ""
	switch c.Action {
	case "check":
		if m.busy || m.activeTransaction() {
			return Receipt{}, problem("update_busy")
		}
		m.state.Status.State = "checking"
		m.state.Status.Reason = ""
		start = "check"
	case "download":
		if m.busy || m.activeTransaction() || m.state.Offer == nil || c.OfferID != m.state.Offer.ID {
			return Receipt{}, problem("offer_unavailable")
		}
		if n, e := updates.CompareVersions(m.state.Offer.Manifest.Version, m.state.Status.Current.Version); e != nil || n <= 0 {
			return Receipt{}, problem("offer_not_newer")
		}
		if m.state.Status.State != "ready" {
			m.state.Status.State = "downloading"
			m.state.Status.Received = 0
			start = "download"
		}
	case "install":
		if c.When != "now" && c.When != "idle" {
			return Receipt{}, problem("invalid_schedule")
		}
		if m.state.Transaction != nil && m.activeTransaction() {
			if c.OfferID != m.state.Transaction.Offer {
				return Receipt{}, problem("update_busy")
			}
			r.TransactionID = m.state.Transaction.ID
			break
		}
		if m.busy || m.state.Status.State != "ready" || m.state.Offer == nil || c.OfferID != m.state.Offer.ID {
			return Receipt{}, problem("offer_unavailable")
		}
		current, e := selected(m.root)
		if e != nil {
			return Receipt{}, e
		}
		previous, e := loadOffer(m.root, current, m.trust, m.platform)
		if e != nil {
			return Receipt{}, e
		}
		if n, e := updates.CompareVersions(m.state.Offer.Manifest.Version, previous.Manifest.Version); e != nil || n <= 0 {
			return Receipt{}, problem("offer_not_newer")
		}
		// The signed whole package selects its own verified Runtime pin. The
		// candidate Bot offers that CLI to Core's existing service owner.
		if _, e = loadOffer(m.root, c.OfferID, m.trust, m.platform); e != nil {
			return Receipt{}, e
		}
		m.state.Transaction = &transaction{rand.Text(), c.OfferID, current, "waiting", c.When}
		r.TransactionID = m.state.Transaction.ID
		m.state.Status.TransactionID = r.TransactionID
		m.state.Status.State = "waiting_idle"
		m.state.Status.Scheduled = c.When == "idle"
	case "cancel":
		tx := m.state.Transaction
		if tx == nil || tx.ID != c.TransactionID || c.Revision != m.state.Status.Revision {
			return Receipt{}, problem("transaction_conflict")
		}
		if tx.Phase != "waiting" && tx.Phase != "requested" {
			return Receipt{}, problem("commit_already_started")
		}
		tx.Phase = "cancelled"
		m.state.Status.State = "ready"
		m.state.Status.Scheduled = false
		m.state.Status.Reason = ""
		r.TransactionID = tx.ID
	case "automatic":
		m.state.Status.Automatic = c.Automatic
	default:
		return Receipt{}, problem("unknown_action")
	}
	m.state.Receipts[c.ID] = record{fingerprint, r}
	if e := m.save(); e != nil {
		r.State = "unknown"
		m.state.Receipts[c.ID] = record{fingerprint, r}
		return Receipt{}, e
	}
	if start != "" {
		m.startWork(start)
	}
	return r, nil
}
func (m *Manager) activeTransaction() bool {
	tx := m.state.Transaction
	return tx != nil && tx.Phase != "healthy" && tx.Phase != "rolled_back" && tx.Phase != "cancelled"
}
func (m *Manager) startWork(action string) {
	m.busy = true
	m.work.Add(1)
	go func() {
		defer m.work.Done()
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
		defer cancel()
		if action == "check" {
			m.mu.Lock()
			current := m.state.Status.Current.Version
			m.mu.Unlock()
			o, e := m.trust.Check(ctx, current, m.platform)
			m.mu.Lock()
			defer m.mu.Unlock()
			m.busy = false
			if e != nil {
				m.state.Status.State = "failed"
				m.state.Status.Reason = Code(e)
			} else if o == nil {
				m.state.Status.State = "no_compatible_update"
				m.state.Status.Reason = ""
				m.state.Offer = nil
				m.state.Status.OfferID = ""
				m.state.Status.Latest = ""
			} else {
				m.state.Offer = o
				m.state.Status.State = "available"
				m.state.Status.Reason = ""
				m.state.Status.OfferID = o.ID
				m.state.Status.Latest = o.Manifest.Version
				m.state.Status.Total = o.Manifest.Length
			}
			m.state.LastCheck = time.Now().Unix()
			if m.save() != nil {
				return
			}
			if o != nil && e == nil && m.state.Status.Automatic {
				m.state.Status.State = "downloading"
				m.state.Status.Received = 0
				if m.save() == nil {
					m.startWork("download")
				}
			}
			return
		}
		m.mu.Lock()
		o := *m.state.Offer
		m.mu.Unlock()
		dir := filepath.Join(m.root, "downloads", o.ID)
		file, e := m.trust.Download(ctx, o, dir, func(n int64) {
			m.mu.Lock()
			m.state.Status.Received = min(n, o.Manifest.Length)
			m.mu.Unlock()
		})
		if e == nil {
			m.mu.Lock()
			m.state.Status.State = "verifying"
			m.mu.Unlock()
			_, e = stage(m.root, file, o, m.trust, m.platform)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.busy = false
		if e == nil {
			e = m.prune()
		}
		if e != nil {
			m.state.Status.State = "failed"
			m.state.Status.Reason = Code(e)
		} else {
			m.state.Status.State = "ready"
			m.state.Status.Reason = ""
		}
		_ = m.save()
	}()
}

func (m *Manager) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status.Automatic && !m.busy && !m.activeTransaction() &&
		time.Now().Unix()-m.state.LastCheck > 86400 && m.state.Status.State != "ready" && m.state.Status.State != "unknown" {
		m.state.Status.State = "checking"
		if m.save() == nil {
			m.startWork("check")
		}
	}
}

// Handoff is polled by the original child. Cancellation and commit share this
// lock, so a late preparation cannot install after cancellation.
func (m *Manager) Handoff(boot string) (Handoff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status.State == "unknown" {
		return Handoff{}, problem("update_unconfirmed")
	}
	if boot != m.boot.ID || !m.ready {
		return Handoff{}, problem("boot_mismatch")
	}
	tx := m.state.Transaction
	if tx == nil || (tx.Phase != "waiting" && tx.Phase != "requested") {
		return Handoff{BootID: boot}, nil
	}
	if tx.Phase == "waiting" {
		tx.Phase = "requested"
		if e := m.save(); e != nil {
			return Handoff{}, e
		}
	}
	return Handoff{BootID: boot, TransactionID: tx.ID, State: "requested"}, nil
}
func (m *Manager) FinishHandoff(h Handoff) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx := m.state.Transaction
	if m.state.Status.State == "unknown" {
		return problem("update_unconfirmed")
	}
	if h.BootID != m.boot.ID || tx == nil || h.TransactionID != tx.ID || tx.Phase != "requested" {
		return problem("handoff_conflict")
	}
	switch h.State {
	case "busy":
		reason := "work_pending"
		if h.Reason == "channel_handoff_required" || h.Reason == "operation_unconfirmed" {
			reason = h.Reason
		}
		if m.state.Status.Reason == reason {
			return nil
		}
		m.state.Status.Reason = reason
		return m.save()
	case "committed":
		tx.Phase = "committed"
		m.state.Status.State = "restarting"
		m.state.Status.Reason = ""
	case "failed":
		tx.Phase = "unknown"
		m.state.Status.State = "unknown"
		m.state.Status.Reason = "shutdown_unconfirmed"
	default:
		return problem("handoff_invalid")
	}
	return m.save()
}

// Recovery never infers success from a dead PID or a selected path. Only a
// persisted successful shutdown permits switching; only Ready permits success.
func (m *Manager) recoverSelection() error {
	tx := m.state.Transaction
	if tx == nil {
		return nil
	}
	switch tx.Phase {
	case "committed", "switching":
		return m.switchRelease()
	case "switched", "rolling_back":
		return m.rollback()
	case "requested":
		tx.Phase = "waiting"
		m.state.Status.State = "waiting_idle"
	}
	return nil
}
func (m *Manager) switchRelease() error {
	tx := m.state.Transaction
	if _, e := loadOffer(m.root, tx.Offer, m.trust, m.platform); e != nil {
		return m.rollback()
	}
	tx.Phase = "switching"
	if e := m.save(); e != nil {
		return e
	}
	if e := selectRelease(m.root, tx.Offer); e != nil {
		return e
	}
	tx.Phase = "switched"
	return m.save()
}
func (m *Manager) rollback() error {
	tx := m.state.Transaction
	if _, e := loadOffer(m.root, tx.Previous, m.trust, m.platform); e != nil {
		tx.Phase = "unknown"
		m.state.Status.State = "unknown"
		m.state.Status.Reason = "rollback_unavailable"
		_ = m.save()
		return problem("rollback_unavailable")
	}
	tx.Phase = "rolling_back"
	if e := m.save(); e != nil {
		return e
	}
	if e := selectRelease(m.root, tx.Previous); e != nil {
		return e
	}
	m.state.Status.State = "rolling_back"
	if m.state.Status.Reason != "runtime_start_failed" && m.state.Status.Reason != "runtime_readback_failed" {
		m.state.Status.Reason = "new_version_unhealthy"
	}
	return m.save()
}
