package machines

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/sshtransport"
)

// Labels are local display metadata, independent of the original pairing intent
// and immutable task routes. Reserve archived labels as well so reconnecting a
// machine cannot silently reuse another environment's name.
func (s *Service) labelAvailableLocked(id, label string) bool {
	for key, p := range s.state.Profiles {
		if key != id && strings.EqualFold(strings.TrimSpace(machineView(p).Name), strings.TrimSpace(label)) {
			return false
		}
	}
	return true
}

func defaultMachineLabel(p profile) (string, bool) {
	if target, ok := sshtransport.Target(p.View.Address); ok {
		if validPeerDisplayName(p.View.RemoteName) {
			return strings.TrimSpace(p.View.RemoteName), false
		}
		return target.Host, true
	}
	hash := sha256.Sum256([]byte(p.View.ID))
	return fmt.Sprintf("bot-%x", hash[:3]), false
}

func (s *Service) uniqueLabelLocked(id, base string) string {
	base = strings.TrimSpace(base)
	label := base
	for n := 2; !s.labelAvailableLocked(id, label); n++ {
		suffix := fmt.Sprintf("-%d", n)
		stem := []rune(base)
		for len(string(stem))+len(suffix) > 80 {
			stem = stem[:len(stem)-1]
		}
		label = string(stem) + suffix
	}
	return label
}

func (s *Service) resolveSSHLabelLocked(p profile) profile {
	if p.LabelPending && validPeerDisplayName(p.View.RemoteName) {
		p.Label = s.uniqueLabelLocked(p.View.ID, p.View.RemoteName)
		p.LabelPending = false
	}
	return p
}

// Upgrade display metadata without changing the saved pairing configuration,
// token, operation IDs, or owner. Explicit names get priority over defaults.
// Fill deterministically on read; persist with the next normal profile save.
func (s *Service) initializeLabelsLocked() {
	ids := make([]string, 0, len(s.state.Profiles))
	for id := range s.state.Profiles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.state.Profiles[ids[i]], s.state.Profiles[ids[j]]
		autoA, autoB := a.View.Name == a.View.Address, b.View.Name == b.View.Address
		if autoA != autoB {
			return !autoA
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		p := s.state.Profiles[id]
		if p.Label != "" {
			continue
		}
		label := p.View.Name
		if label == p.View.Address || !validPeerDisplayName(label) {
			label, p.LabelPending = defaultMachineLabel(p)
		}
		p.Label = s.uniqueLabelLocked(id, label)
		s.state.Profiles[id] = p
	}
}
