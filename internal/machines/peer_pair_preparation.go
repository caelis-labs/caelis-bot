package machines

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
	"os"
	"strings"
	"time"
	"unicode"
)

func (s *Service) botConnectionReference(p profile, address string) api.BotConnectionReference {
	return api.BotConnectionReference{ID: p.View.ID, Address: address, Peer: p.Peer.Peer, Pairing: p.PairingEpoch, Proof: api.WorkFingerprint(struct{ Source, Token string }{s.state.ProfileID, p.Peer.Token}), Configuration: api.WorkFingerprint(struct {
		Address, Name string
		Archived      bool
	}{p.View.Address, p.View.Name, p.View.Archived})}
}

// Persist identity and the exact connection intent before external delivery.
// This does not contact B, create native work, or wait for human authorization.
func (s *Service) PrepareBotConnection(in api.BotConnectionInput, _ *api.BotConnectionReference) (api.BotConnectionReference, error) {
	address, err := botEndpoint(in.Address)
	if err != nil {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "invalid_bot_address"}
	}
	name := strings.TrimSpace(in.Name)
	if len(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) {
		return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "invalid_bot_name"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return api.BotConnectionReference{}, s.loadErr
	}
	id := in.ID
	if _, exists := s.state.Profiles[id]; !exists {
		for key, p := range s.state.Profiles {
			if !p.Legacy && p.View.Address == address {
				id = key
				break
			}
		}
	}
	if id == "" {
		id = "bot-" + rand.Text()
	}
	old, exists := s.state.Profiles[id]
	next := old
	if !exists {
		sourceName, _ := os.Hostname()
		if len(sourceName) > 80 || strings.ContainsFunc(sourceName, unicode.IsControl) {
			sourceName = ""
		}
		if name == "" {
			name = address
		}
		next = profile{PairingEpoch: workerpeer.NewPairingID(), PairingName: sourceName, Peer: workerpeer.Connection{Address: address, Token: rand.Text() + rand.Text()}, View: api.Machine{ID: id, Name: name, Address: address, ConnectionType: "bot", State: "authorization", PairingStatus: "unknown"}}
	} else {
		if next.Legacy {
			return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "machine_legacy"}
		}
		if next.Peer.Token == "" {
			return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "original_peer_proof_required"}
		}
		if name == "" || name == machineView(old).Name {
			name = old.View.Name
		} else {
			if !s.labelAvailableLocked(id, name) {
				return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "duplicate_bot_name"}
			}
			next.Label, next.LabelPending = name, false
		}
		next.View.Name = name
		if address != old.Peer.Address && old.Peer.Peer == "" {
			if s.pairingActive[id] > 0 {
				return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "bot_connection_target_unconfirmed"}
			}
			// No durable target pin and no active call proves no request write was
			// dispatched. The user's corrected address is still this original intent.
			next.Peer.Address = address
			next.View.Address = address
		}
		next.View.Archived = old.View.Archived && !in.Reconnect && in.ID != ""
		if (in.Reconnect || old.View.Archived && in.ID == "") && (old.View.PairingStatus == "rejected" || old.View.PairingStatus == "expired" || old.View.PairingStatus == "revoked") {
			next.PairingEpoch = workerpeer.NewPairingID()
			next.View.PairingStatus = "unknown"
		}
	}
	if !exists {
		label := strings.TrimSpace(in.Name)
		if label == "" {
			label, next.LabelPending = defaultMachineLabel(next)
			label = s.uniqueLabelLocked(id, label)
		} else if !s.labelAvailableLocked(id, label) {
			return api.BotConnectionReference{}, &api.ActionRejectedError{Code: "duplicate_bot_name"}
		}
		next.Label = label
	}
	meta := &api.BotConnectionPreparation{Read: exists && old.View.Archived && (old.View.PairingStatus == "approved" || old.View.PairingStatus == "pending" || old.View.PairingStatus == "unknown"), Created: !exists, Rename: exists && !in.Reconnect && !(old.View.Archived && in.ID == "") && address == old.Peer.Address}
	s.state.Profiles[id] = next
	if err = s.save(); err != nil {
		if exists {
			s.state.Profiles[id] = old
		} else {
			delete(s.state.Profiles, id)
		}
		return api.BotConnectionReference{}, err
	}
	ref := s.botConnectionReference(next, address)
	ref.Preparation = meta
	return ref, nil
}
func (s *Service) ConnectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	ref, err := s.PrepareBotConnection(in, nil)
	if err != nil {
		return api.Machine{}, err
	}
	v, _, err := s.ConnectPreparedBot(ctx, ref, in)
	return v, err
}
func (s *Service) ConnectPreparedBot(ctx context.Context, ref api.BotConnectionReference, in api.BotConnectionInput) (api.Machine, api.BotConnectionReference, error) {
	s.mu.Lock()
	p, ok := s.state.Profiles[ref.ID]
	if !ok || p.PairingEpoch != ref.Pairing || s.botConnectionReference(p, ref.Address).Proof != ref.Proof || s.botConnectionReference(p, ref.Address).Configuration != ref.Configuration {
		s.mu.Unlock()
		return api.Machine{}, ref, &api.ActionRejectedError{Code: "bot_connection_changed"}
	}
	if ref.Preparation != nil && ref.Preparation.Rename {
		s.mu.Unlock()
		return machineView(p), ref, nil
	}
	if s.pairingActive == nil {
		s.pairingActive = map[string]int{}
	}
	s.pairingActive[ref.ID]++
	source := s.state.ProfileID
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.pairingActive[ref.ID]--; s.mu.Unlock() }()
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var receipt workerpeer.PairingReceipt
	var err error
	if p.Peer.Peer == "" {
		identity, probeErr := s.peerIdentity(c, ref.Address)
		if probeErr != nil {
			err = &api.ActionRejectedError{Code: probeErr.Error()}
		} else {
			s.mu.Lock()
			current, exists := s.state.Profiles[ref.ID]
			if !exists || current != p {
				err = errors.New("bot_connection_changed")
			} else {
				pinned := p
				pinned.Peer.Peer = identity.Peer
				pinned.View.PeerID = identity.Peer
				s.state.Profiles[ref.ID] = pinned
				if saveErr := s.save(); saveErr != nil {
					s.state.Profiles[ref.ID] = p
					err = saveErr
				} else {
					p = pinned
					ref.Peer = identity.Peer
				}
			}
			s.mu.Unlock()
		}
	}
	if err != nil {
		// No pairing write follows a failed identity pin.
	} else if ref.Address != p.Peer.Address || ref.Preparation != nil && ref.Preparation.Read {
		receipt, err = s.pairRead(c, ref.Address, p.PairingEpoch, source, p.Peer.Token, p.Peer.Peer)
	} else {
		receipt, err = s.pairRequest(c, p.Peer.Address, p.PairingEpoch, source, p.PairingName, p.Peer.Token, p.Peer.Peer)
	}
	if err != nil && ref.Preparation != nil && ref.Preparation.Created {
		var rejected *api.ActionRejectedError
		if errors.As(err, &rejected) {
			s.mu.Lock()
			current, ok := s.state.Profiles[ref.ID]
			if ok && current == p {
				delete(s.state.Profiles, ref.ID)
				if saveErr := s.save(); saveErr != nil {
					s.state.Profiles[ref.ID] = p
					s.mu.Unlock()
					return p.View, ref, errors.Join(err, saveErr)
				}
			}
			s.mu.Unlock()
			return p.View, ref, err
		}
	}
	value, err := s.applyPairingReceipt(c, p, receipt, err, ref.Address)
	if err != nil {
		var rejected *api.ActionRejectedError
		if errors.As(err, &rejected) {
			return value, ref, err
		}
		return value, ref, &api.BotConnectionUnconfirmedError{Reference: ref, Cause: err}
	}
	return value, ref, nil
}
func (s *Service) applyPairingReceipt(ctx context.Context, p profile, receipt workerpeer.PairingReceipt, readErr error, target string) (api.Machine, error) {
	next := p
	if readErr == nil && p.Peer.Peer != "" && p.Peer.Peer != receipt.Peer {
		readErr = errors.New("bot_peer_identity_changed")
	}
	if readErr == nil {
		next.Peer.Address = target
		next.View.Address = target
		if next.SavedAddress == target {
			next.SavedAddress = ""
		}
		next.View.PairingStatus = receipt.Status
		next.Peer.Peer = receipt.Peer
		next.View.PeerID = receipt.Peer
		next.View.CheckedAt = time.Now().UnixMilli()
		next.View.State = "authorization"
		next.View.Issue = "peer_pairing_" + receipt.Status
		if validPeerDisplayName(receipt.Name) {
			next.View.RemoteName = receipt.Name
		}
		if receipt.Status == "approved" {
			out, err := s.peerCall(ctx, next.Peer, workerpeer.Request{Action: "info"})
			if err == nil && !workerpeer.Compatible(out.Capabilities) {
				err = errors.New("bot_worker_contract_incompatible")
			}
			next = botCheckResult(next, out, err)
			// Approval is an exact connection receipt, independent of current model
			// readiness. Only a ready grant can be offered for new delegation.
		}
	} else {
		next.View.State = "offline"
		next.View.Issue = readErr.Error()
		var rejected *api.ActionRejectedError
		if errors.As(readErr, &rejected) && rejected.Code == "invalid_pairing_request" {
			// A complete native refusal proves this intent was not admitted.
			// Explicit Reconnect may replace this rejected ID, never unknown writes.
			next.View.PairingStatus = "rejected"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Profiles[p.View.ID]
	comparison := current
	comparison.Label, comparison.LabelPending, comparison.SavedAddress = p.Label, p.LabelPending, p.SavedAddress
	if !exists || comparison != p {
		return machineView(current), errors.New("bot_connection_changed")
	}
	next.Label, next.LabelPending = current.Label, current.LabelPending
	if current.SavedAddress != p.SavedAddress {
		next.SavedAddress = current.SavedAddress
	}
	next = s.resolveSSHLabelLocked(next)
	s.state.Profiles[p.View.ID] = next
	if err := s.save(); err != nil {
		s.state.Profiles[p.View.ID] = current
		return current.View, err
	}
	if next.View.State == "ready" {
		s.renewBotObservationsLocked(p.View.ID)
	}
	return machineView(next), readErr
}
func (s *Service) ReadBotConnectionReceipt(ctx context.Context, ref api.BotConnectionReference) (api.Machine, error) {
	s.mu.Lock()
	p, ok := s.state.Profiles[ref.ID]
	source := s.state.ProfileID
	active := s.pairingActive[ref.ID] > 0
	if s.pairingActive == nil {
		s.pairingActive = map[string]int{}
	}
	s.pairingActive[ref.ID]++
	valid := ok && p.PairingEpoch == ref.Pairing && s.botConnectionReference(p, ref.Address).Proof == ref.Proof
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.pairingActive[ref.ID]--; s.mu.Unlock() }()
	if !valid {
		return api.Machine{}, errors.New("original_peer_proof_unavailable")
	}
	if ref.Preparation != nil && ref.Preparation.Rename {
		if s.botConnectionReference(p, ref.Address).Configuration != ref.Configuration {
			return machineView(p), errors.New("original_bot_intent_unconfirmed")
		}
		return machineView(p), nil
	}
	if p.Peer.Peer == "" && !active {
		v := machineView(p)
		v.Issue = "pairing_request_not_dispatched"
		return v, &api.ActionRejectedError{Code: "pairing_request_not_dispatched"}
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	receipt, err := s.pairRead(c, ref.Address, p.PairingEpoch, source, p.Peer.Token, p.Peer.Peer)
	if err == nil && ref.Peer != "" && receipt.Peer != ref.Peer {
		return machineView(p), errors.New("bot_peer_identity_changed")
	}
	if err == nil && receipt.Status == "unknown" {
		return machineView(p), errors.New("pairing_request_unknown")
	}
	return s.applyPairingReceipt(c, p, receipt, err, ref.Address)
}
func (s *Service) inspectBot(ctx context.Context, id string, recoverWork, renew bool) (api.Machine, error) {
	p, ok := s.machineProfile(id)
	if !ok {
		return api.Machine{}, errors.New("bot_connection_unknown")
	}
	if recoverWork && p.View.PairingStatus == "approved" {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		_, err := s.peerCall(c, p.Peer, workerpeer.Request{Action: "recover"})
		if err != nil {
			return p.View, err
		}
	}
	ref := s.botConnectionReference(p, p.Peer.Address)
	v, err := s.ReadBotConnectionReceipt(ctx, ref)
	if ctx.Err() != nil {
		return v, ctx.Err()
	}
	if recoverWork {
		return v, err
	}
	return v, nil
}
func (s *Service) RecoverBotWork(ctx context.Context, id string) (api.Machine, error) {
	return s.inspectBot(ctx, id, true, true)
}
func (s *Service) BotConnectionQuiescent(ref api.BotConnectionReference) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pairingActive[ref.ID] == 0
}
func (s *Service) AbortPreparedBotConnection(ref api.BotConnectionReference) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[ref.ID]
	if !ok || ref.Preparation == nil || !ref.Preparation.Created || s.pairingActive[ref.ID] != 0 || p.PairingEpoch != ref.Pairing || s.botConnectionReference(p, ref.Address).Proof != ref.Proof {
		return nil
	}
	delete(s.state.Profiles, ref.ID)
	if err := s.save(); err != nil {
		s.state.Profiles[ref.ID] = p
		return err
	}
	return nil
}
