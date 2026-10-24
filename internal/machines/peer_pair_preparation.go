package machines

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

// Prepare the existing profile/proof/intent before Web records admission. No
// network or grant write is allowed here. A crashed host can query this exact
// epoch; a later grant still cannot certify it.
func (s *Service) PrepareBotConnection(in api.BotConnectionInput, expected *api.BotConnectionReference) (api.BotConnectionReference, error) {
	name := strings.TrimSpace(in.Name)
	if len(name) > 80 {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "invalid_bot_name"}
	}
	address, e := workerpeer.Address(in.Address)
	if e != nil {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "invalid_bot_address"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return api.BotConnectionReference{}, s.loadErr
	}
	id := in.ID
	if id == "" {
		for key, p := range s.state.Profiles {
			if p.View.ConnectionType == "bot" && p.Peer.Address == address {
				id = key
				break
			}
		}
	}
	if id == "" {
		id = "bot-" + rand.Text()
	}
	old, exists := s.state.Profiles[id]
	if exists && old.View.ConnectionType != "bot" {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "machine_connection_type_conflict"}
	}
	if expected != nil {
		if !exists || expected.ID != id || old.PairingEpoch != expected.Pairing || s.botConnectionReference(old, expected.Address).Proof != expected.Proof {
			return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "original_bot_authorization_changed"}
		}
		if e = s.validateBotAuthorizationCodeLocked(old, *expected, in.Code); e != nil {
			return api.BotConnectionReference{}, e
		}
	}
	if name == "" {
		name = address
		if exists {
			name = old.View.Name
		}
	}
	next := old
	if !exists {
		next.View = api.Machine{ID: id, ConnectionType: "bot", Name: name, Address: address, State: "offline"}
		next.Peer = workerpeer.Connection{Address: address, Token: rand.Text() + rand.Text()}
	}
	if next.Peer.Token == "" {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "original_peer_proof_required"}
	}
	meta := &api.BotConnectionPreparation{PreviousPairing: old.PairingEpoch, PreviousCodeDigest: old.PairingCodeDigest, Retained: old.RetainPairingProof, Created: !exists, Name: name}
	meta.Rename = exists && next.Peer.Address == address && strings.TrimSpace(in.Code) == "" && !in.Reconnect
	if strings.TrimSpace(in.Code) != "" {
		if old.RetainPairingProof && old.PairingCodeDigest == pairingCodeDigest(next.Peer.Token, in.Code) {
			return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "new_pairing_code_required"}
		}
		next.PairingEpoch = rand.Text()
		next.PairingCodeDigest = pairingCodeDigest(next.Peer.Token, in.Code)
		next.RetainPairingProof = true
	} else if !exists {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "new_pairing_code_required"}
	}
	s.state.Profiles[id] = next
	if e = s.save(); e != nil {
		if exists {
			s.state.Profiles[id] = old
		} else {
			delete(s.state.Profiles, id)
		}
		return api.BotConnectionReference{}, e
	}
	ref := s.botConnectionReference(next, address)
	meta.Configuration = ref.Configuration
	ref.Preparation = meta
	if meta.Rename {
		desired := next
		desired.View.Name = name
		ref.Configuration = s.botConnectionReference(desired, address).Configuration
	}
	return ref, nil
}
func (s *Service) ConnectPreparedBot(ctx context.Context, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	if in.ID != "" && in.ID != ref.ID {
		return api.Machine{}, ref, errors.New("original_bot_authorization_changed")
	}
	in.ID = ref.ID
	v, e := s.connectBot(ctx, in, nil, &ref)
	return v, ref, e
}

// Historical pairing uncertainty has no active native execution to drain. It
// never becomes an application health/update dependency, regardless of epochs.
func (s *Service) BotConnectionQuiescent(ref api.BotConnectionReference) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pairingActive[ref.ID] == 0
}

// Only the host that failed ingress admission may call this: no external pair
// was invoked. CAS protects a concurrent newer intent, grant or task binding.
// An uncertain/sent pairing never takes this rollback path.
func (s *Service) AbortPreparedBotConnection(ref api.BotConnectionReference) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, exists := s.state.Profiles[ref.ID]
	m := ref.Preparation
	if !exists || m == nil || s.pairingActive[ref.ID] != 0 || p.PairingEpoch != ref.Pairing || s.botConnectionReference(p, ref.Address).Proof != ref.Proof || s.botConnectionReference(p, ref.Address).Configuration != m.Configuration {
		return nil
	}
	if m.Created {
		for _, machine := range s.state.Routes {
			if machine == ref.ID {
				return nil
			}
		}
		delete(s.state.Profiles, ref.ID)
	} else {
		next := p
		next.PairingEpoch, next.PairingCodeDigest, next.RetainPairingProof = m.PreviousPairing, m.PreviousCodeDigest, m.Retained
		s.state.Profiles[ref.ID] = next
	}
	if e := s.save(); e != nil {
		s.state.Profiles[ref.ID] = p
		return e
	}
	return nil
}
