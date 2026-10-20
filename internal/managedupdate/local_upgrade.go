package managedupdate

import (
	"context"
	"crypto/rand"
	"github.com/caelis-labs/caelis-bot/internal/updates"
)

// ScheduleLocalUpgrade is the installer's explicit --upgrade path. The service
// must already be stopped: the inherited lease also excludes an orphan Bot.
// Only stage and schedule here. On restart the old Bot performs the existing
// quiescent channel/Core handoff; this command never fabricates that receipt.
func ScheduleLocalUpgrade(ctx context.Context, root, archive string, offer Offer, trust Trust, platform string) error {
	lease, err := acquireLease(root)
	if err != nil {
		return err
	}
	defer lease.Close()
	m, err := Open(ctx, root, platform, trust)
	if err != nil {
		return err
	}
	defer m.Close()
	current, err := selected(root)
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
	if _, err = stage(root, archive, offer, trust, platform); err != nil {
		return err
	}
	m.state.Offer = &offer
	m.state.Status.OfferID = offer.ID
	m.state.Status.Latest = offer.Manifest.Version
	m.state.Status.State = "ready"
	if err = m.save(); err != nil {
		return err
	}
	_, err = m.Command(Command{ID: rand.Text(), Action: "install", OfferID: offer.ID, When: "idle"})
	return err
}
