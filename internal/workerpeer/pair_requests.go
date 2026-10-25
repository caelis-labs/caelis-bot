package workerpeer

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// These are connection intents in the existing authorization registry, never
// execution receipts. Pending intents contain only a hash, not a bearer token.
type pairingRequest struct {
	Source    string `json:"source"`
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	ExpiresAt int64  `json:"expiresAt"`
	Status    string `json:"status"`
}
type PairingRequest struct {
	Contract int    `json:"contract"`
	Target   string `json:"target,omitempty"`
	ID       string `json:"id"`
	Source   string `json:"source"`
	Name     string `json:"name,omitempty"`
	Read     bool   `json:"read,omitempty"`
}
type PairingReceipt struct {
	Contract int    `json:"contract"`
	ID       string `json:"id"`
	Peer     string `json:"peer"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

func requestFingerprint(v pairingRequest) string { return tokenHash(v.Source + "\x00" + v.Hash) }
func requestStatus(v pairingRequest) string {
	if v.Status == "pending" && time.Now().Unix() >= v.ExpiresAt {
		return "expired"
	}
	return v.Status
}
func (s *Server) pairingRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var in PairingRequest
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || in.Contract != api.WorkerContract || !validID(in.ID) || !validID(in.Source) || len(token) < 40 || len(token) > 128 || !validID(token) {
		http.Error(w, "invalid_pairing_request", 400)
		return
	}
	name := strings.TrimSpace(in.Name)
	if len(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) {
		http.Error(w, "invalid_peer_name", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !in.Read && in.Target != s.state.Identity {
		http.Error(w, "bot_peer_identity_changed", 409)
		return
	}
	value, exists := s.state.Requests[in.ID]
	hash := tokenHash(token)
	// Both an original pending request and an existing/revoked source require
	// the same proof. A new name or B-side approval cannot take over old tasks.
	if exists && (value.Source != in.Source || value.Hash != hash || !in.Read && value.Name != name) {
		http.Error(w, "pairing_request_conflict", 409)
		return
	}
	if _, ok := s.state.Grants[in.Source]; !ok && len(s.state.Bindings[in.Source]) > 0 {
		http.Error(w, "original_peer_proof_required", 403)
		return
	}
	if old, ok := s.state.Grants[in.Source]; ok && old.Hash != hash {
		http.Error(w, "original_peer_proof_required", 403)
		return
	}
	if !exists && !in.Read {
		pending := 0
		for _, v := range s.state.Requests {
			if requestStatus(v) == "pending" {
				pending++
				if v.Source == in.Source {
					http.Error(w, "pairing_request_pending", 409)
					return
				}
			}
		}
		if pending >= 16 || len(s.state.Requests) >= 128 {
			http.Error(w, "pairing_requests_full", 429)
			return
		}
		address, _, _ := net.SplitHostPort(r.RemoteAddr)
		value = pairingRequest{Source: in.Source, Hash: hash, Name: name, Address: address, ExpiresAt: time.Now().Add(5 * time.Minute).Unix(), Status: "pending"}
		s.state.Requests[in.ID] = value
		if err := s.save(); err != nil {
			delete(s.state.Requests, in.ID)
			http.Error(w, "pairing_not_saved", 503)
			return
		}
		exists = true
	}
	status := "unknown"
	if exists {
		status = requestStatus(value)
		if status == "approved" {
			g, ok := s.state.Grants[in.Source]
			if !ok || g.Hash != hash || g.Revoked {
				status = "revoked"
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PairingReceipt{Contract: api.WorkerContract, ID: in.ID, Peer: s.state.Identity, Name: machineDisplayName(), Status: status})
}

// Only the authenticated local App/Web settings boundary calls this method.
// Native readiness is outside the registry lock and grants no access by itself.
func (s *Server) DecidePairing(ctx context.Context, in api.WorkerPairingDecision) error {
	s.mu.Lock()
	v, ok := s.state.Requests[in.ID]
	oldGrant := s.state.Grants[v.Source]
	if !ok {
		s.mu.Unlock()
		return &api.ActionRejectedError{Code: "pairing_request_unknown"}
	}
	status := requestStatus(v)
	if status != "pending" {
		s.mu.Unlock()
		if status == "approved" && in.Allow || status == "rejected" && !in.Allow {
			return nil
		}
		return &api.ActionRejectedError{Code: "pairing_request_closed"}
	}
	s.mu.Unlock()
	if in.Allow {
		if err := s.PairingReadiness(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Requests[in.ID]
	if !exists || current != v || requestStatus(current) != "pending" || s.state.Grants[v.Source] != oldGrant {
		return &api.ActionRejectedError{Code: "pairing_request_changed"}
	}
	oldGrants := s.state.Grants
	if in.Allow {
		grants := make(map[string]grant, len(oldGrants)+1)
		for id, g := range oldGrants {
			grants[id] = g
		}
		g := oldGrant
		g.Peer = v.Source
		g.Hash = v.Hash
		g.Name = v.Name
		g.RequestID = in.ID
		g.Revoked = false
		if g.PairedAt == 0 {
			g.PairedAt = time.Now().UnixMilli()
		}
		grants[v.Source] = g
		s.state.Grants = grants
		current.Status = "approved"
	} else {
		current.Status = "rejected"
	}
	s.state.Requests[in.ID] = current
	if err := s.save(); err != nil {
		s.state.Grants = oldGrants
		s.state.Requests[in.ID] = v
		return errors.New("pairing_not_saved")
	}
	return nil
}
