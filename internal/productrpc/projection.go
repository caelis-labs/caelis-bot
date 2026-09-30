package productrpc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type projection struct{ key []byte }

func (p projection) handle(kind, id string) string {
	if id == "" {
		return ""
	}
	h := hmac.New(sha256.New, p.key)
	h.Write([]byte(kind + "\x00" + id))
	return hex.EncodeToString(h.Sum(nil))
}

func (p projection) approvalTarget(a api.Approval) string {
	b, _ := json.Marshal(a)
	return p.handle("approval-target", string(b))
}

func (p projection) state(scope Scope, cursor Cursor, port Port) (State, error) {
	// Clone before rewriting; a wire projection must not mutate native state.
	b, err := json.Marshal(port.Snapshot())
	if err != nil || len(b) > MaxSnapshotBytes {
		return State{}, errors.New("snapshot exceeds product limit")
	}
	var v api.Snapshot
	if err := json.Unmarshal(b, &v); err != nil {
		return State{}, err
	}
	targets := make(map[string]string)
	v.CurrentTurn = p.handle("turn", v.CurrentTurn)
	v.PreviewKey = p.handle("preview", v.PreviewKey)
	for i := range v.Items {
		item := &v.Items[i]
		item.ID = p.handle("item", item.ID)
		item.TurnKey = p.handle("turn", item.TurnKey)
		for j := range item.Artifacts {
			item.Artifacts[j].ID = p.handle("resource", item.Artifacts[j].ID)
		}
		if item.Screen != nil {
			for j := range item.Screen.Images {
				item.Screen.Images[j].ID = p.handle("screen", item.Screen.Images[j].ID)
			}
		}
	}
	for i := range v.Approvals {
		a := &v.Approvals[i]
		id := p.handle("approval", a.ID)
		targets[id] = p.approvalTarget(*a)
		a.ID = id
	}
	for i := range v.Reviews {
		v.Reviews[i].ID = p.handle("review", v.Reviews[i].ID)
	}
	for i := range v.References {
		v.References[i].ID = p.handle("reference", v.References[i].ID)
	}
	draft := port.Draft()
	draft.ReferenceIDs = append([]string(nil), draft.ReferenceIDs...)
	for i := range draft.ReferenceIDs {
		draft.ReferenceIDs[i] = p.handle("reference", draft.ReferenceIDs[i])
	}
	return State{Scope: scope, Cursor: cursor, Snapshot: v, Draft: draft, Initialization: port.BotInitialization(), ApprovalTargets: targets}, nil
}

func (p projection) decision(v api.Snapshot, d ApprovalDecision) (api.Decision, bool) {
	for _, a := range v.Approvals {
		if p.handle("approval", a.ID) != d.ID || p.approvalTarget(a) != d.Target || a.Status != "pending" {
			continue
		}
		choice := d.Choice == "" && len(a.Questions) != 0
		for _, c := range a.Choices {
			if c.ID == d.Choice {
				choice = true
			}
		}
		if !choice {
			return api.Decision{}, false
		}
		for key := range d.Answers {
			found := false
			for _, q := range a.Questions {
				if q.ID == key {
					found = true
				}
			}
			if !found {
				return api.Decision{}, false
			}
		}
		d.Decision.ID = a.ID
		return d.Decision, true
	}
	return api.Decision{}, false
}

func (p projection) references(v api.Snapshot, ids []string) ([]string, bool) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		found := false
		for _, ref := range v.References {
			if p.handle("reference", ref.ID) == id {
				out = append(out, ref.ID)
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return out, true
}
