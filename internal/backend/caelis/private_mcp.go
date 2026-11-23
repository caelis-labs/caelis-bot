package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const privateTasksService = "caelis_tasks"
const privateTasksPrefix = privateTasksService + "__"

var privateTasksTools = []string{"bot_delegate", "bot_tasks"}

type privateInvocationKey struct{}

type callbackClaim struct {
	client     *client
	generation uint64
	call       wire.SessionToolCall
}

type callbackClaimKey struct{}

func withoutPrivateMCP(servers []wire.SessionMCPServer) []wire.SessionMCPServer {
	return slices.DeleteFunc(slices.Clone(servers), func(server wire.SessionMCPServer) bool {
		return server.Name == privateTasksService
	})
}

func privateApplicationCatalog(profile wire.SessionProfile) wire.SessionProfile {
	profile.Tools = slices.DeleteFunc(slices.Clone(profile.Tools), func(tool wire.SessionToolDefinition) bool {
		return slices.Contains(privateTasksTools, tool.Name)
	})
	raw, _ := json.Marshal(profile.Tools)
	profile.ToolsVersion = digest(raw)
	return profile
}

// Core's public MCP request carries only name and arguments. Its authenticated
// Application callback supplies the native Session, principal and opaque claim.
type residentMCPTools struct {
	*Session
	callTool func(context.Context, string, json.RawMessage) api.ToolResult
}

func (h residentMCPTools) Definitions() []api.ToolDefinition { return nil }

func (h residentMCPTools) CallTool(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	claim, ok := ctx.Value(callbackClaimKey{}).(callbackClaim)
	tool, scoped := residentMCPName(name)
	digest := argumentsDigest(args)
	if !ok || !scoped || claim.call.Name != name || digest == "" || digest != nativeArgumentsDigest(claim.call.Arguments) || !h.admitResidentMCP(ctx, claim) {
		return api.ToolResult{IsError: true, Content: []map[string]string{{"type": "text", "text": "The original resident tool call could not be confirmed."}}}
	}
	return h.callTool(context.WithValue(ctx, privateInvocationKey{}, claim), tool, args)
}

func residentMCPName(name string) (string, bool) {
	tool, prefixed := strings.CutPrefix(name, privateTasksPrefix)
	return tool, prefixed && slices.Contains(privateTasksTools, tool)
}

func residentMCPCatalog(profile wire.SessionProfile, catalog map[string]api.ApplicationTools, handler residentMCPTools) (wire.SessionProfile, map[string]api.ApplicationTools) {
	result := privateApplicationCatalog(profile)
	handlers := make(map[string]api.ApplicationTools, len(catalog))
	for _, d := range result.Tools {
		handlers[d.Name] = catalog[d.Name]
	}
	for _, d := range profile.Tools {
		if slices.Contains(privateTasksTools, d.Name) {
			d.Name = privateTasksPrefix + d.Name
			d.ApprovalPolicy = pointer("required")
			result.Tools = append(result.Tools, d)
			handlers[d.Name] = handler
		}
	}
	sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].Name < result.Tools[j].Name })
	raw, _ := json.Marshal(result.Tools)
	result.ToolsVersion = digest(raw)
	return result, handlers
}

func (s *Session) residentMCPClaimCurrentLocked(claim callbackClaim) bool {
	call := claim.call
	return s.connected && !s.closed && !s.retainedWorkers && claim.client != nil && s.client == claim.client && s.generation == claim.generation &&
		call.SessionId != "" && call.PrincipalId != "" &&
		call.SessionId == s.state.Session.SessionId && call.PrincipalId == s.state.PrincipalID &&
		call.State == "claimed" && call.TurnId != "" && call.ItemId != "" && call.Id != "" && call.CallId != ""
}

func (s *Session) admitResidentMCP(ctx context.Context, claim callbackClaim) bool {
	s.mu.Lock()
	current := s.residentMCPClaimCurrentLocked(claim)
	permissions := s.state.Session.Profile.Permissions
	s.mu.Unlock()
	if !current || ctx.Err() != nil {
		return false
	}
	// Core emits the claimed callback only after its own tool policy allows it.
	// For manual mode also require Bot's durable, committed original decision.
	if permissions != nil && value(permissions.ApprovalMode) == "auto-review" {
		return true
	}
	proof, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	call := claim.call
	args := nativeArgumentsDigest(call.Arguments)
	if args == "" {
		return false
	}
	for {
		s.mu.Lock()
		if !s.residentMCPClaimCurrentLocked(claim) || proof.Err() != nil {
			s.mu.Unlock()
			return false
		}
		var original, outcome string
		for op, j := range s.state.Operations {
			if j.ManualMCP != nil && j.ManualMCP.matches(call, args) &&
				j.Path == "/sessions/"+idPath(call.SessionId)+"/approvals/"+idPath(j.ManualMCP.RequestID)+"/resolve" {
				if original != "" {
					s.mu.Unlock()
					return false
				}
				original, outcome = op, j.Outcome
			}
		}
		s.mu.Unlock()
		switch outcome {
		case "committed":
			return true
		case "unknown", "accepted":
			_ = s.recoverOperation(proof, original)
		default:
			return false
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-proof.Done():
			return false
		}
	}
}

type manualMCPAction struct {
	SessionID, PrincipalID, TurnID, RequestID, CallID, Name, Arguments string
}

type manualMCPActionKey struct{}

func (a manualMCPAction) matches(call wire.SessionToolCall, args string) bool {
	return a.SessionID == call.SessionId && a.PrincipalID == call.PrincipalId && a.TurnID == call.TurnId &&
		a.CallID == call.CallId && a.Name == call.Name && a.Arguments == args
}

func (a manualMCPAction) matchesReceipt(receipt wire.CommandResult) bool {
	return (value(receipt.SessionId) == "" || value(receipt.SessionId) == a.SessionID) &&
		(receipt.Target == nil || value(receipt.Target.TurnId) == "" || value(receipt.Target.TurnId) == a.TurnID)
}

func captureManualMCPAction(sid, principal string, a *wire.ActiveApproval, p nativePermission) (*manualMCPAction, error) {
	if a.Subject == nil || a.Subject.Tool.State != wire.ApprovalFieldStateKnown {
		return nil, nil
	}
	name := value(a.Subject.Tool.Text)
	if _, scoped := residentMCPName(name); !scoped {
		return nil, nil
	}
	args := argumentsDigest(p.ToolCall.RawInput)
	if sid == "" || principal == "" || a.RequestId == "" || a.Target == nil || a.Target.TurnId == "" || p.ToolCall.ID == "" || args == "" {
		return nil, errors.New("original resident tool approval identity is incomplete")
	}
	return &manualMCPAction{SessionID: sid, PrincipalID: principal, TurnID: a.Target.TurnId, RequestID: a.RequestId,
		CallID: p.ToolCall.ID, Name: name, Arguments: args}, nil
}

func nativeArgumentsDigest(args wire.JSONValue) string {
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return argumentsDigest(raw)
}

func argumentsDigest(raw json.RawMessage) string {
	var args map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if !json.Valid(raw) || d.Decode(&args) != nil || args == nil {
		return ""
	}
	canonical, _ := json.Marshal(args)
	return digest(canonical)
}
