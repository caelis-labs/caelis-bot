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
func Pair(ctx context.Context, address, code, peer string, oldToken ...string) (PairResult, error) {
	address, err := Address(address)
	if err != nil {
		return PairResult{}, err
	}
	token := rand.Text() + rand.Text()
	if len(oldToken) > 0 && oldToken[0] != "" {
		token = oldToken[0]
	}
	b, _ := json.Marshal(PairRequest{Code: code, Peer: peer, Token: token})
	req, err := http.NewRequestWithContext(ctx, "POST", address+Prefix+"pair", bytes.NewReader(b))
	if err != nil {
		return PairResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(oldToken) > 0 && oldToken[0] != "" {
		req.Header.Set("Authorization", "Bearer "+oldToken[0])
	}
	resp, err := transport().Do(req)
	if err != nil {
		return PairResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return PairResult{}, errors.New("bot_pairing_failed: " + resp.Status)
	}
	var out PairResult
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return out, err
	}
	out.Address = address
	if out.Peer == "" || out.Token == "" || !Compatible(out.Capabilities) {
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
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Response{}, errors.New("bot_peer_unavailable: " + resp.Status)
	}
	var out Response
	if err = json.NewDecoder(io.LimitReader(resp.Body, 230<<20)).Decode(&out); err != nil {
		return out, err
	}
	err = check(c, out)
	if err != nil && out.Rejected {
		if in.Action == "send" {
			err = &api.WorkMessageRejection{Cause: err}
		} else if in.Action == "decision" {
			err = &api.DecisionValidationError{Message: err.Error()}
		}
	}
	return out, err
}
func check(c Connection, out Response) error {
	if out.Contract != api.WorkerContract {
		return errors.New("bot_worker_contract_incompatible")
	}
	if c.Peer != "" && c.Peer != out.Peer {
		return errors.New("bot_peer_identity_changed")
	}
	if out.Error != "" {
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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("worker_observation_unavailable")
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
		if err = check(c, out); err != nil {
			return err
		}
		observe(decodeObservation(out.Observation))
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}
func DecodeState(v State) api.WorkState                   { return decodeState(v) }
func DecodeObservation(v Observation) api.WorkObservation { return decodeObservation(v) }
