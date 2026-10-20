package managedupdate

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Called only by the installation owner with no download worker running.
// Preserve current + one rollback version + current candidate, never product
// state or unrecognized files. A private installer namespace owns .stage-*.
func (m *Manager) prune() error {
	current, err := selected(m.root)
	if err != nil {
		return err
	}
	keep := map[string]bool{current: true}
	if m.state.Offer != nil {
		keep[m.state.Offer.ID] = true
	}
	if tx := m.state.Transaction; tx != nil {
		keep[tx.Previous] = true
		if m.activeTransaction() {
			keep[tx.Offer] = true
		}
	}
	for _, area := range []string{"versions", "downloads"} {
		dir := filepath.Join(m.root, area)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = privateDirectory(dir); err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(dir, name)
			if !entry.IsDir() || privateDirectory(path) != nil {
				continue
			}
			remove := area == "versions" && strings.HasPrefix(name, ".stage-")
			if hexID.MatchString(name) {
				if area == "downloads" {
					remove = true
				} else if !keep[name] {
					_, err = loadOffer(m.root, name, m.trust, m.platform)
					remove = err == nil
				}
			}
			if remove {
				if err = os.RemoveAll(path); err != nil {
					return err
				}
				if err = localstate.SyncParent(path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
