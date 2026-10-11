package weixin

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ObserveTurn records terminal state while it is visible. The polling bridge
// can otherwise miss a short completed Turn when the next Turn starts.
func (b *Bridge) ObserveTurn(snapshot api.Snapshot) {
	if snapshot.CurrentTurn == "" || !terminalTurn(snapshot.Phase) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.TurnPhases[snapshot.CurrentTurn] == snapshot.Phase {
		return
	}
	prior, existed := b.state.TurnPhases[snapshot.CurrentTurn]
	oldOrder := append([]string(nil), b.state.TurnOrder...)
	evicted, evictedPhase := "", ""
	if !existed {
		b.state.TurnOrder = append(b.state.TurnOrder, snapshot.CurrentTurn)
	}
	b.state.TurnPhases[snapshot.CurrentTurn] = snapshot.Phase
	if len(b.state.TurnOrder) > 256 {
		evicted = b.state.TurnOrder[0]
		evictedPhase = b.state.TurnPhases[evicted]
		delete(b.state.TurnPhases, evicted)
		b.state.TurnOrder = b.state.TurnOrder[1:]
	}
	if b.saveLocked() != nil {
		b.state.TurnOrder = oldOrder
		if evicted != "" {
			b.state.TurnPhases[evicted] = evictedPhase
		}
		if existed {
			b.state.TurnPhases[snapshot.CurrentTurn] = prior
		} else {
			delete(b.state.TurnPhases, snapshot.CurrentTurn)
		}
	}
}

func terminalTurn(phase string) bool {
	switch phase {
	case "completed", "failed", "interrupted", "unknown":
		return true
	default:
		return false
	}
}
