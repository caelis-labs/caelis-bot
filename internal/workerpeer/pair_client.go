package workerpeer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"io"
	"net/http"
	"strings"
)

// Sending creates only an inbox item on B. It never grants execution and never
// waits for a human; the same ID/body is idempotent and does not renew expiry.
func RequestPairing(ctx context.Context, address, id, source, name, proof, target string) (PairingReceipt, error) {
	return pairingCall(ctx, address, PairingRequest{ID: id, Source: source, Name: name, Target: target}, proof)
}
func ReadPairing(ctx context.Context, address, id, source, proof string) (PairingReceipt, error) {
	return pairingCall(ctx, address, PairingRequest{ID: id, Source: source, Read: true}, proof)
}
func pairingCall(ctx context.Context, address string, in PairingRequest, proof string) (PairingReceipt, error) {
	address, err := Address(address)
	if err != nil {
		return PairingReceipt{}, &api.ActionRejectedError{Code: "invalid_bot_address"}
	}
	in.Contract = api.WorkerContract
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, "POST", address+Prefix+"pair", bytes.NewReader(b))
	if err != nil {
		return PairingReceipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+proof)
	resp, err := transport().Do(req)
	if err != nil {
		return PairingReceipt{}, errors.New("bot_peer_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
		if readErr != nil || len(body) > 4096 || resp.StatusCode < 400 || resp.StatusCode >= 500 {
			return PairingReceipt{}, errors.New("bot_peer_unreachable")
		}
		code := strings.TrimSpace(string(body))
		switch code {
		case "bot_peer_identity_changed", "pairing_request_conflict", "pairing_request_pending", "pairing_requests_full", "original_peer_proof_required", "invalid_pairing_request", "invalid_peer_name":
			return PairingReceipt{}, &api.ActionRejectedError{Code: code}
		}
		return PairingReceipt{}, errors.New("bot_peer_unreachable")
	}
	var out PairingReceipt
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil || out.Contract != api.WorkerContract || out.ID != in.ID || out.Peer == "" {
		return PairingReceipt{}, errors.New("bot_worker_contract_incompatible")
	}
	switch out.Status {
	case "pending", "approved", "rejected", "expired", "revoked", "unknown":
	default:
		return PairingReceipt{}, errors.New("bot_worker_contract_incompatible")
	}
	return out, nil
}

// Identity is a bounded public declaration, not a grant or native readiness
// probe. The machines owner durably pins it before submitting an intent.
func PeerIdentity(ctx context.Context, address string) (PairingReceipt, error) {
	address, err := Address(address)
	if err != nil {
		return PairingReceipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address+Prefix+"identity", nil)
	if err != nil {
		return PairingReceipt{}, err
	}
	resp, err := transport().Do(req)
	if err != nil {
		return PairingReceipt{}, errors.New("bot_peer_unreachable")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	var out PairingReceipt
	if err != nil || len(data) > 4096 || resp.StatusCode != 200 {
		return out, errors.New("bot_peer_unreachable")
	}
	if json.Unmarshal(data, &out) != nil || out.Contract != api.WorkerContract || !validID(out.Peer) {
		return out, errors.New("bot_worker_contract_incompatible")
	}
	return out, nil
}
