package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
	"io"
	"net/http"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

const connectionPrefix = "/v1/node/connections/"

func allowedNodeConnection(path string) bool {
	switch path {
	case connectionPrefix + "begin", connectionPrefix + "catalog", connectionPrefix + "setup-catalog", connectionPrefix + "start", connectionPrefix + "advance", connectionPrefix + "wait", connectionPrefix + "cancel", connectionPrefix + "close":
		return true
	}
	return false
}

type nodeConnectionBeginRequest struct {
	Guard       api.NodeEditGuard `json:"guard"`
	OperationID string            `json:"operationId"`
}
type nodeConnectionRequest struct {
	Ref        api.NodeRuntimeConnectionRef `json:"ref"`
	Kind       string                       `json:"kind,omitempty"`
	Action     string                       `json:"action,omitempty"`
	Provider   string                       `json:"provider,omitempty"`
	BaseURL    string                       `json:"baseUrl,omitempty"`
	Input      *api.RuntimeConnectionInput  `json:"input,omitempty"`
	FlowAction *api.RuntimeFlowAction       `json:"flowAction,omitempty"`
	FlowID     string                       `json:"flowId,omitempty"`
	After      int                          `json:"after,omitempty"`
}

func handleNodeConnections(agent nodeplane.CatalogAgent, w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, connectionPrefix) {
		return false
	}
	port, ok := agent.(api.NodeRuntimeConnectionController)
	if !ok {
		http.Error(w, "node connection unavailable", 503)
		return true
	}
	var out any
	var err error
	if r.URL.Path == connectionPrefix+"begin" {
		var input nodeConnectionBeginRequest
		if strictDecode(r.Body, &input) != nil {
			http.Error(w, "invalid node request", 400)
			return true
		}
		out, err = port.BeginNodeRuntimeConnection(r.Context(), input.Guard, input.OperationID)
	} else {
		var input nodeConnectionRequest
		if strictDecode(r.Body, &input) != nil || !validConnectionRef(input.Ref) {
			http.Error(w, "invalid node request", 400)
			return true
		}
		// Each endpoint has one closed request shape: extraneous recognized fields
		// must not become an implicit command union or future authority fallback.
		expected := nodeConnectionRequest{Ref: input.Ref}
		switch r.URL.Path {
		case connectionPrefix + "catalog":
			expected.Kind = input.Kind
		case connectionPrefix + "setup-catalog":
			expected.Action = input.Action
			expected.Provider = input.Provider
			expected.BaseURL = input.BaseURL
		case connectionPrefix + "start":
			expected.Input = input.Input
		case connectionPrefix + "advance":
			expected.FlowAction = input.FlowAction
		case connectionPrefix + "wait":
			expected.FlowID = input.FlowID
			expected.After = input.After
		case connectionPrefix + "cancel":
			expected.FlowID = input.FlowID
		}
		actualJSON, _ := json.Marshal(input)
		expectedJSON, _ := json.Marshal(expected)
		if string(actualJSON) != string(expectedJSON) {
			http.Error(w, "invalid node request", 400)
			return true
		}
		switch r.URL.Path {
		case connectionPrefix + "catalog":
			out, err = port.NodeRuntimeConnectionCatalog(r.Context(), input.Ref, input.Kind)
		case connectionPrefix + "setup-catalog":
			out, err = port.NodeRuntimeSetupCatalog(r.Context(), input.Ref, input.Action, input.Provider, input.BaseURL)
		case connectionPrefix + "start":
			if input.Input == nil {
				http.Error(w, "invalid node request", 400)
				return true
			}
			out, err = port.StartNodeRuntimeConnection(r.Context(), input.Ref, *input.Input)
		case connectionPrefix + "advance":
			if input.FlowAction == nil {
				http.Error(w, "invalid node request", 400)
				return true
			}
			out, err = port.AdvanceNodeRuntimeConnection(r.Context(), input.Ref, *input.FlowAction)
		case connectionPrefix + "wait":
			out, err = port.WaitNodeRuntimeConnection(r.Context(), input.Ref, input.FlowID, input.After)
		case connectionPrefix + "cancel":
			err = port.CancelNodeRuntimeConnection(r.Context(), input.Ref, input.FlowID)
			out = struct{}{}
		case connectionPrefix + "close":
			err = port.CloseNodeRuntimeConnection(r.Context(), input.Ref)
			out = struct{}{}
		}
	}
	if err != nil {
		failure := NodeConnectionError{Code: "unavailable", Unknown: true}
		var typed *NodeConnectionError
		if errors.As(err, &typed) {
			failure.Unknown = typed.Unknown
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(failure)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
	return true
}
func (c *Client) connectionRequest(ctx context.Context, path string, input nodeConnectionRequest, out any) error {
	if !validConnectionRef(input.Ref) || input.Ref.NodeID != c.expected {
		return connectionError("scope changed")
	}
	return c.nodeConnectionRPC(ctx, connectionPrefix+path, input, out)
}
func (c *Client) BeginNodeRuntimeConnection(ctx context.Context, g api.NodeEditGuard, id string) (api.NodeRuntimeConnectionRef, error) {
	ref := api.NodeRuntimeConnectionRef{NodeID: g.NodeID, Backend: g.Backend, OperationID: id}
	if !validConnectionRef(ref) || ref.NodeID != c.expected || g.Revision == "" {
		return ref, connectionError("scope changed")
	}
	var out api.NodeRuntimeConnectionRef
	if err := c.nodeConnectionRPC(ctx, connectionPrefix+"begin", nodeConnectionBeginRequest{Guard: g, OperationID: id}, &out); err != nil {
		return ref, err
	}
	if out != ref {
		return ref, connectionError("original scope changed")
	}
	return out, nil
}
func (c *Client) NodeRuntimeConnectionCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	var out api.RuntimeConnectionCatalog
	err := c.connectionRequest(ctx, "catalog", nodeConnectionRequest{Ref: ref, Kind: kind}, &out)
	return out, err
}
func (c *Client) NodeRuntimeSetupCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	var out []api.SetupChoice
	err := c.connectionRequest(ctx, "setup-catalog", nodeConnectionRequest{Ref: ref, Action: action, Provider: provider, BaseURL: baseURL}, &out)
	return out, err
}
func (c *Client) StartNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	if input.Settings != nil {
		return api.RuntimeFlow{}, connectionError("settings override unavailable")
	}
	var out api.RuntimeFlow
	err := c.connectionRequest(ctx, "start", nodeConnectionRequest{Ref: ref, Input: &input}, &out)
	return out, err
}
func (c *Client) AdvanceNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	var out api.RuntimeFlow
	err := c.connectionRequest(ctx, "advance", nodeConnectionRequest{Ref: ref, FlowAction: &action}, &out)
	return out, err
}
func (c *Client) WaitNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, id string, after int) (api.RuntimeFlow, error) {
	var out api.RuntimeFlow
	err := c.connectionRequest(ctx, "wait", nodeConnectionRequest{Ref: ref, FlowID: id, After: after}, &out)
	return out, err
}
func (c *Client) CancelNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, id string) error {
	var out struct{}
	return c.connectionRequest(ctx, "cancel", nodeConnectionRequest{Ref: ref, FlowID: id}, &out)
}
func (c *Client) CloseNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef) error {
	var out struct{}
	return c.connectionRequest(ctx, "close", nodeConnectionRequest{Ref: ref}, &out)
}

// nodeConnectionRPC preserves safe uncertainty through the framed paired port.
// No SDK error body or arbitrary remote diagnostic is ever exposed to the APP.
func (c *Client) nodeConnectionRPC(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil || len(body) > productrpc.MaxCommandBytes {
		return connectionError("input unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1"+path, bytes.NewReader(body))
	if err != nil {
		return connectionError("input unavailable")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return connectionError("original response unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure NodeConnectionError
		decoder := json.NewDecoder(io.LimitReader(response.Body, 4097))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&failure) == nil && decoder.Decode(new(any)) == io.EOF && failure.Code == "unavailable" {
			return &failure
		}
		return connectionError("original response unavailable")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, productrpc.MaxSnapshotBytes+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(output) != nil || decoder.Decode(new(any)) != io.EOF {
		return connectionError("original response unavailable")
	}
	return nil
}
