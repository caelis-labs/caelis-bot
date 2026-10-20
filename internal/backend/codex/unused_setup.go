package codex

// CanLeaveUnsubmittedSetup is read-only. It does not recover/replace the owner,
// clear its ledger, or adopt any unknown effects. Only an unused setup thread
// whose exact private process has exited can be left during first onboarding.
func (s *Session) CanLeaveUnsubmittedSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &s.binding
	return s.loadErr == nil && !s.closed && !s.closing && s.client == nil && s.retainedOwner == nil &&
		b.Unsubmitted && b.OwnerUnavailable && !b.OwnerClosing && s.probePrivateOwnerLocked() == ownerGone &&
		b.Pending == nil && b.LastReceipt == nil && s.run == "" &&
		len(b.Tasks) == 0 && len(b.Children) == 0 && len(b.CleanupTargets) == 0 &&
		len(b.PendingApprovalIDs) == 0 && b.CapacityStopRequest == "" && b.StopState == "" &&
		len(b.StopRuns) == 0 && b.ToolRenewal == nil && !b.TrustWriteUnknown &&
		len(b.LostOwners) == 0 && len(b.LostOwnerTasks) == 0 && len(b.PastThreads) == 0 &&
		len(b.QuestionClosures) == 0 && len(b.HostReportTurns) == 0 && len(b.ScheduledOrigins) == 0 &&
		b.CapacityStopThread == "" && b.CapacityStopTurn == "" && !b.CapacityStopAccepted &&
		b.CapacityStopPhase == "" && b.CapacityStopStatus == "" &&
		len(b.Scheduled) == 0 && len(b.HostReports) == 0 && len(b.BackgroundResults) == 0
}
