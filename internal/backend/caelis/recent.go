package caelis

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// RecentSnapshot uses the existing event-published snapshot, filtering before
// cloning. It neither refreshes history nor becomes a second state owner.
func (s *Session) RecentSnapshot() api.Snapshot {
	var v api.Snapshot
	if cached := s.cached.Load(); cached != nil {
		v = *cached
	} else {
		v = s.Snapshot()
	}
	v.Items = api.RecentItems(v.Items, v.CurrentTurn)
	out := clone(v)
	out.RuntimeOwner = v.RuntimeOwner
	out.InputSources = append([]api.InputSource(nil), v.InputSources...)
	out.ScheduledSources = append([]api.ScheduledSource(nil), v.ScheduledSources...)
	out.ReportSources = append([]api.ReportSource(nil), v.ReportSources...)
	for i := range out.Approvals {
		out.Approvals[i].TaskID = v.Approvals[i].TaskID
		out.Approvals[i].NativeID = v.Approvals[i].NativeID
	}
	return out
}
