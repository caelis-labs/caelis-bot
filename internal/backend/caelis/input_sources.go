package caelis

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// inputScope is recorded when a user envelope arrives on an authenticated,
// session-checked native feed. It is persisted with that item, never backfilled
// from the current binding or a historical view's position in a snapshot.
type inputScope struct {
	InstanceID string `json:"instanceID"`
	SessionID  string `json:"sessionID"`
	TurnID     string `json:"turnID"`
}

func (scope inputScope) owner() string {
	if scope.InstanceID == "" || scope.SessionID == "" {
		return ""
	}
	h := sha256.Sum256([]byte("caelis\x00" + scope.InstanceID + "\x00" + scope.SessionID))
	return hex.EncodeToString(h[:16])
}

func (s *Session) appendInputSources(out *api.Snapshot, v *view, sid string) {
	if sid == "" || v.State.SessionId != sid {
		return
	}
	for _, item := range out.Items {
		scope, ok := v.InputScopes[item.ID]
		j := s.state.Operations[item.RequestID]
		if !ok || item.Kind != "user" || item.RequestID == "" || item.TurnKey == "" || scope.SessionID != sid || scope.TurnID != item.TurnKey || j.Scheduled || j.hostNotice() {
			continue
		}
		if owner := scope.owner(); owner != "" {
			out.InputSources = append(out.InputSources, api.InputSource{RuntimeOwner: owner, TurnKey: item.TurnKey, RequestID: item.RequestID})
		}
	}
}
