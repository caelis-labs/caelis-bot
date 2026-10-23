package workerpeer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func Address(address string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.Host == "" || !(u.Scheme == "http" || u.Scheme == "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !(u.Path == "" || u.Path == "/") {
		return "", errors.New("invalid_bot_address")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func transport() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("peer_redirect_denied") }}
}

// Connection diagnostics use only fixed protocol codes. HTTP and native error
// bodies can contain paths or arbitrary private text.
func peerError(code string) error {
	switch strings.TrimSpace(code) {
	case "pairing_code_expired", "pairing_code_invalid", "peer_authorization_revoked", "peer_authentication_required", "original_peer_proof_required", "worker_sharing_disabled", "peer_has_worker_bindings", "worker_not_owned", "worker_operation_unsupported", "remote_native_url_not_reachable":
		return errors.New(strings.TrimSpace(code))
	case "pairing_code_invalid_or_expired":
		return errors.New("pairing_code_invalid")
	case "original_peer_revoked":
		return errors.New("original_peer_proof_required")
	case "worker_contract_incompatible", "worker_contract_unavailable", "original_worker_contract_incompatible", "bot_worker_contract_incompatible":
		return errors.New("bot_worker_contract_incompatible")
	}
	if code == "worker_not_ready" || strings.HasPrefix(code, "worker_not_ready:") {
		cause := strings.TrimSpace(strings.TrimPrefix(code, "worker_not_ready:"))
		if cause == "worker_contract_incompatible" || cause == "worker_contract_unavailable" {
			return errors.New("bot_worker_contract_incompatible")
		}
		return errors.New("worker_not_ready")
	}
	return errors.New("worker_operation_failed")
}
func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	classified := peerError(strings.TrimSpace(string(body)))
	if classified.Error() != "worker_operation_failed" {
		return classified
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return errors.New("peer_authentication_required")
	case http.StatusConflict:
		return errors.New("worker_not_ready")
	case http.StatusNotFound, http.StatusBadRequest, http.StatusForbidden:
		return errors.New("bot_worker_contract_incompatible")
	default:
		return errors.New("bot_peer_unreachable")
	}
}
func Pair(ctx context.Context, address, code, peer string, oldToken ...string) (PairResult, error) {
	proof := ""
	if len(oldToken) > 0 {
		proof = oldToken[0]
	}
	return PairNamed(ctx, address, code, peer, "", proof)
}
func PairNamed(ctx context.Context, address, code, peer, name, proof string) (PairResult, error) {
	address, err := Address(address)
	if err != nil {
		return PairResult{}, err
	}
	token := rand.Text() + rand.Text()
	if proof != "" {
		token = proof
	}
	b, _ := json.Marshal(PairRequest{Code: code, Peer: peer, Name: name, Token: token})
	req, err := http.NewRequestWithContext(ctx, "POST", address+Prefix+"pair", bytes.NewReader(b))
	if err != nil {
		return PairResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if proof != "" {
		req.Header.Set("Authorization", "Bearer "+proof)
	}
	resp, err := transport().Do(req)
	if err != nil {
		return PairResult{}, errors.New("bot_peer_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return PairResult{}, responseError(resp)
	}
	var out PairResult
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return out, errors.New("bot_worker_contract_incompatible")
	}
	out.Address = address
	if out.Peer == "" || out.Token != token || !Compatible(out.Capabilities) {
		return PairResult{}, errors.New("bot_worker_contract_incompatible")
	}
	return out, nil
}
func Compatible(c api.WorkCapabilities) bool {
	return c.Contract == api.WorkerContract && c.Owner != "" && c.Build != "" && c.Runtime != "" && c.History && c.Observe && c.Interactions && c.Files && c.Reconcile
}
func Call(ctx context.Context, c Connection, in Request) (Response, error) {
	in.Contract = api.WorkerContract
	if in.Action == "start" {
		in.Start.Machine = ""
		in.Start.ExecutionOverride = nil
		digests, err := api.WorkInputDigests(in.Start.Files)
		if err != nil {
			return Response{}, err
		}
		in.Start.FileDigests = digests
		in.Files, err = readFiles(in.Start.Files)
		if err != nil {
			return Response{}, err
		}
	}
	if in.Action == "send" {
		digests, err := api.WorkInputDigests(in.Message.Files)
		if err != nil {
			return Response{}, err
		}
		in.Message.FileDigests = digests
		in.Files, err = readFiles(in.Message.Files)
		if err != nil {
			return Response{}, err
		}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Address+Prefix+"call", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := transport().Do(req)
	if err != nil {
		return Response{}, errors.New("bot_peer_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Response{}, responseError(resp)
	}
	var out Response
	if err = json.NewDecoder(io.LimitReader(resp.Body, 230<<20)).Decode(&out); err != nil {
		return out, errors.New("bot_worker_contract_incompatible")
	}
	err = check(c, out, in.Action)
	if err != nil && out.Rejected && out.Contract == api.WorkerContract && (c.Peer == "" || c.Peer == out.Peer) {
		if in.Action == "send" {
			err = &api.WorkMessageRejection{Cause: err}
		} else if in.Action == "decision" {
			err = &api.DecisionValidationError{Message: err.Error()}
		}
	}
	return out, err
}
func check(c Connection, out Response, action string) error {
	if out.Contract != api.WorkerContract {
		return errors.New("bot_worker_contract_incompatible")
	}
	if c.Peer != "" && c.Peer != out.Peer {
		return errors.New("bot_peer_identity_changed")
	}
	if out.Error != "" {
		if action == "info" || action == "recover" {
			return peerError(out.Error)
		}
		// Original task-operation errors and Rejected remain adapter semantics,
		// rather than being reclassified as connection/readiness evidence.
		return errors.New(out.Error)
	}
	return nil
}
func readFiles(files []api.InputFile) ([]api.WorkFileContent, error) {
	out := []api.WorkFileContent{}
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		if len(data) > api.MaxWorkFileBytes {
			return nil, errors.New("worker_file_too_large")
		}
		out = append(out, api.WorkFileContent{Name: f.Name, Data: data})
	}
	return out, nil
}

// Watch reconnects by requesting a fresh bounded snapshot; Last-Event-ID never
// replays native writes. The caller owns reconnect/backoff and original routing.
func Watch(ctx context.Context, c Connection, id string, observe func(api.WorkObservation)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Address+Prefix+"events?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := transport().Do(req)
	if err != nil {
		return errors.New("bot_peer_unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return responseError(resp)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var out Response
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &out) != nil {
			return errors.New("worker_observation_invalid")
		}
		if err = check(c, out, "observe"); err != nil {
			return err
		}
		observe(decodeObservation(out.Observation))
	}
	if err = scanner.Err(); err != nil {
		return errors.New("bot_peer_unreachable")
	}
	return io.EOF
}
func DecodeState(v State) api.WorkState                   { return decodeState(v) }
func DecodeObservation(v Observation) api.WorkObservation { return decodeObservation(v) }
