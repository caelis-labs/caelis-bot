package managedupdate

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/updates"
)

type localUpgrade struct {
	Archive   string `json:"archive"`
	Manifest  []byte `json:"manifest"`
	Signature []byte `json:"signature"`
}
type localUpgradeState struct {
	Status  Status `json:"status"`
	Healthy bool   `json:"healthy"`
}

func (m *Manager) localUpgradeState(id string) localUpgradeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := selected(m.root)
	return localUpgradeState{Status: m.state.Status, Healthy: err == nil && current == id && m.ready && m.state.Status.State != "unknown" && m.boot.Identity == m.state.Status.Current}
}

// ScheduleLocalUpgrade keeps a running owner alive through its normal handoff.
// A live upgrade returns success only after the candidate's health receipt; the
// installer can then atomically replace the on-disk launcher for its next start.
// No signal/stop is sent to the live launcher or Bot.
func ScheduleLocalUpgrade(ctx context.Context, root, archive string, offer Offer, trust Trust, platform string) error {
	lease, err := acquireLease(root)
	if Code(err) == "installation_owned" {
		c, e := NewClient(root)
		if e != nil {
			return e
		}
		defer c.http.CloseIdleConnections()
		// Never replay after a transport failure. A subsequent explicit
		// installer run reads/reuses the same signed offer's transaction.
		if e = c.call("POST", "/local-upgrade", localUpgrade{archive, offer.Bytes, offer.Signature}, &struct{}{}); e != nil {
			return e
		}
		return waitLocalUpgrade(ctx, c, offer)
	}
	if err != nil {
		return err
	}
	defer lease.Close()
	m, err := Open(ctx, root, platform, trust)
	if err != nil {
		return err
	}
	defer m.Close()
	// No running owner (including an orphan child) holds this installation.
	// Stage only: on service start the old Bot still performs the handoff.
	return m.scheduleLocal(localUpgrade{archive, offer.Bytes, offer.Signature})
}

func (m *Manager) scheduleLocal(in localUpgrade) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	offer, err := Verify(in.Manifest, in.Signature, m.trust.Key, m.platform, m.trust.QA)
	if err != nil {
		return err
	}
	current, err := selected(m.root)
	if err != nil {
		return err
	}
	if current == offer.ID {
		return nil
	}
	if n, e := updates.CompareVersions(offer.Manifest.Version, m.state.Status.Current.Version); e != nil || n <= 0 {
		return problem("offer_not_newer")
	}
	if m.activeTransaction() {
		if m.state.Transaction.Offer == offer.ID {
			return nil
		}
		return problem("update_busy")
	}
	if m.state.Status.State == "unknown" {
		return problem("update_unconfirmed")
	}
	if m.busy {
		return problem("update_busy")
	}
	if _, err = stage(m.root, in.Archive, offer, m.trust, m.platform); err != nil {
		return err
	}
	m.state.Offer = &offer
	m.state.Status.OfferID = offer.ID
	m.state.Status.Latest = offer.Manifest.Version
	m.state.Status.State = "ready"
	if err = m.save(); err != nil {
		return err
	}
	_, err = m.commandLocked(Command{ID: rand.Text(), Action: "install", OfferID: offer.ID, When: "idle"})
	return err
}

func waitLocalUpgrade(ctx context.Context, c *Client, offer Offer) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		var observed localUpgradeState
		if err := c.call("GET", "/local-upgrade/"+offer.ID, nil, &observed); err != nil {
			return err
		}
		s := observed.Status
		if observed.Healthy && s.Current == offer.Manifest.Identity() {
			return nil
		}
		switch s.State {
		case "unknown":
			return problem("update_unconfirmed")
		case "rolled_back":
			return problem("candidate_rolled_back")
		case "waiting_idle":
			if s.Reason != "" {
				return problem("update_scheduled_waiting_idle")
			}
		}
		select {
		case <-ctx.Done():
			return problem("update_scheduled_waiting_idle")
		case <-tick.C:
		}
	}
}
