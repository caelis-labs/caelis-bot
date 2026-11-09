package backend

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// PetSnapshotUpdate keeps observing while the bubble is hidden. Revisions avoid
// rebuilding unchanged previews; actions still go through the native owner.
func (s *Service) PetSnapshotUpdate(revision uint64, botStatus, processEpoch string) api.ChatUpdate {
	if s.snapshotUnchanged(revision, botStatus, processEpoch) {
		return api.ChatUpdate{}
	}
	v := s.PetSnapshot()
	if processEpoch == v.ProcessEpoch && revision != 0 && v.Revision == revision && v.BotStatus == botStatus {
		return api.ChatUpdate{}
	}
	return api.ChatUpdate{Changed: true, Snapshot: v}
}
