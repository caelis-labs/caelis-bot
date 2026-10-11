package weixin

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ObserveTurn records terminal state while it is visible. Codex clears
// CurrentTurn at completion, so bind that terminal phase to the exact latest
// accepted ingress or a previously observed running Turn.
func (b *Bridge) ObserveTurn(snapshot api.Snapshot) {
	reportTurn := b.reportSnapshotTurn(snapshot)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.state.HasInput || b.state.OwnerID == "" {
		return // a newly paired channel must not prime historical Turn replay
	}
	if b.state.AcceptedInputID == "" && snapshot.LastReceipt.Outcome == "accepted" && b.state.Inputs[snapshot.LastReceipt.ID] == "accepted" {
		// An older ledger predates AcceptedInputID. Recover only from the
		// original accepted ingress and the Runtime's matching receipt.
		b.state.AcceptedInputID = snapshot.LastReceipt.ID
		if b.saveLocked() != nil {
			b.state.AcceptedInputID = ""
			return
		}
	}
	if !terminalTurn(snapshot.Phase) {
		if snapshot.CurrentTurn != "" && b.state.ObservedTurn != snapshot.CurrentTurn {
			old := b.state.ObservedTurn
			b.state.ObservedTurn = snapshot.CurrentTurn
			if b.saveLocked() != nil {
				b.state.ObservedTurn = old
			}
		}
		return
	}
	turn := reportTurn
	if turn == "" {
		turn = terminalSnapshotTurn(snapshot, b.state.AcceptedInputID, b.state.ObservedTurn)
	}
	if snapshot.Phase == "completed" && !completedReplyForTurn(snapshot.Items, turn) {
		return
	}
	if turn == "" || b.state.TurnPhases[turn] == snapshot.Phase {
		return
	}
	prior, existed := b.state.TurnPhases[turn]
	oldObserved := b.state.ObservedTurn
	oldOrder := append([]string(nil), b.state.TurnOrder...)
	evicted, evictedPhase := "", ""
	evictedOrigin := ""
	if !existed {
		b.state.TurnOrder = append(b.state.TurnOrder, turn)
	}
	b.state.TurnPhases[turn] = snapshot.Phase
	b.state.ObservedTurn = ""
	if len(b.state.TurnOrder) > 256 {
		evicted = b.state.TurnOrder[0]
		evictedPhase = b.state.TurnPhases[evicted]
		evictedOrigin = b.state.TurnOrigins[evicted]
		delete(b.state.TurnPhases, evicted)
		if !b.taskTurnReferencedLocked(evicted) {
			delete(b.state.TurnOrigins, evicted)
		}
		b.state.TurnOrder = b.state.TurnOrder[1:]
	}
	if b.saveLocked() != nil {
		b.state.TurnOrder = oldOrder
		b.state.ObservedTurn = oldObserved
		if evicted != "" {
			b.state.TurnPhases[evicted] = evictedPhase
			if evictedOrigin != "" {
				b.state.TurnOrigins[evicted] = evictedOrigin
			}
		}
		if existed {
			b.state.TurnPhases[turn] = prior
		} else {
			delete(b.state.TurnPhases, turn)
		}
	}
}

// A native report receipt can identify a completed Bot Turn after restart,
// when CurrentTurn and the prior in-memory observation have disappeared.
func (b *Bridge) reportSnapshotTurn(snapshot api.Snapshot) string {
	if b.host.TaskForReport == nil || snapshot.LastReceipt.ID == "" || snapshot.LastReceipt.Outcome != "accepted" || b.host.TaskForReport(snapshot.LastReceipt.ID) == "" {
		return ""
	}
	for i := len(snapshot.Items) - 1; i >= 0; i-- {
		item := snapshot.Items[i]
		if item.Kind == "hostNotice" && item.RequestID == snapshot.LastReceipt.ID && item.TurnKey != "" {
			return item.TurnKey
		}
	}
	return ""
}

func terminalSnapshotTurn(snapshot api.Snapshot, inputID, observed string) string {
	if snapshot.CurrentTurn != "" {
		return snapshot.CurrentTurn
	}
	if inputID != "" && snapshot.LastReceipt.ID == inputID && snapshot.LastReceipt.Outcome == "accepted" {
		for i := len(snapshot.Items) - 1; i >= 0; i-- {
			item := snapshot.Items[i]
			if item.Kind == "user" && item.RequestID == inputID && item.TurnKey != "" {
				return item.TurnKey
			}
		}
	}
	if observed != "" {
		for i := len(snapshot.Items) - 1; i >= 0; i-- {
			item := snapshot.Items[i]
			if item.TurnKey == "" || item.Kind != "user" && item.Kind != "assistant" {
				continue
			}
			if item.TurnKey == observed {
				return observed
			}
			return "" // a newer scoped Turn superseded the observed one
		}
	}
	return ""
}

func completedReplyForTurn(items []api.Item, turn string) bool {
	if turn == "" {
		return false
	}
	for _, item := range items {
		if item.TurnKey == turn && item.Kind == "assistant" && item.Text != "" && (item.Status == "completed" || item.Status == "accepted") {
			return true
		}
	}
	return false
}

func terminalTurn(phase string) bool {
	switch phase {
	case "completed", "failed", "interrupted", "unknown":
		return true
	default:
		return false
	}
}
