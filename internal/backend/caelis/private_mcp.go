package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

const privateTasksService = "caelis_tasks"

var privateTasksTools = []string{"bot_delegate", "bot_tasks"}

type privateMCP struct {
	session, principal, instance string
	client                       *client
	server                       wire.SessionMCPServer
	revoke                       func()
}

type privateInvocationKey struct{}

func withoutPrivateMCP(servers []wire.SessionMCPServer) []wire.SessionMCPServer {
	return slices.DeleteFunc(slices.Clone(servers), func(server wire.SessionMCPServer) bool {
		return server.Name == privateTasksService
	})
}

func (p *privateMCP) servers(servers []wire.SessionMCPServer) []wire.SessionMCPServer {
	if p == nil {
		return servers
	}
	return append(withoutPrivateMCP(servers), p.server)
}

func (p *privateMCP) close() {
	if p != nil {
		p.revoke()
	}
}

func privateApplicationCatalog(profile wire.SessionProfile) wire.SessionProfile {
	profile.Tools = slices.DeleteFunc(slices.Clone(profile.Tools), func(tool wire.SessionToolDefinition) bool {
		return slices.Contains(privateTasksTools, tool.Name)
	})
	raw, _ := json.Marshal(profile.Tools)
	profile.ToolsVersion = digest(raw)
	return profile
}

func (s *Session) openPrivateMCP(b wire.SessionBinding) (*privateMCP, error) {
	s.mu.Lock()
	tools, client, instance := s.tools, s.client, s.state.InstanceID
	s.mu.Unlock()
	if tools == nil || tools.OpenPrivateStdio == nil {
		return nil, nil
	}
	// A Worker never runs this application assembly.
	scope := &privateMCP{session: b.SessionId, principal: b.PrincipalId, client: client, instance: instance}
	launch, err := tools.OpenPrivateStdio(api.ToolService{Name: privateTasksService, Tools: privateTasksTools}, func(ctx context.Context, name string, args json.RawMessage) (context.Context, error) {
		return s.admitPrivateMCP(ctx, scope, name, args)
	})
	if err != nil {
		return nil, err
	}
	scope.server = wire.SessionMCPServer{Name: privateTasksService, Transport: "stdio", Command: pointer(tools.Command), Args: append(slices.Clone(tools.Args), launch.Args...), WorkDir: pointer(launch.WorkDir)}
	scope.revoke = launch.Revoke
	return scope, nil
}

func (s *Session) privateMPCCurrentLocked(scope *privateMCP) bool {
	return scope != nil && s.privateMCP == scope && s.connected && !s.closed && s.client == scope.client &&
		s.state.Session.SessionId == scope.session && s.state.PrincipalID == scope.principal && s.state.InstanceID == scope.instance
}

func (s *Session) admitPrivateMCP(ctx context.Context, scope *privateMCP, name string, args json.RawMessage) (context.Context, error) {
	unauthorized := errors.New("reviewed Bot invocation unavailable")
	s.mu.Lock()
	owned := s.privateMPCCurrentLocked(scope)
	s.mu.Unlock()
	if !owned || !slices.Contains(privateTasksTools, name) {
		return nil, unauthorized
	}
	var input map[string]any
	if json.Unmarshal(args, &input) != nil || input == nil {
		return nil, unauthorized
	}
	proof, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var configuration wire.SessionConfiguration
	if err := scope.client.json(proof, "GET", "/sessions/"+idPath(scope.session)+"/configuration", nil, &configuration, "", ""); err != nil {
		return nil, err
	}
	if configuration.SessionId != scope.session || configuration.Profile == nil ||
		!slices.ContainsFunc(configuration.Profile.McpServers, func(server wire.SessionMCPServer) bool { return reflect.DeepEqual(server, scope.server) }) {
		return nil, unauthorized
	}
	var state wire.SessionState
	if err := scope.client.json(proof, "GET", "/sessions/"+idPath(scope.session)+"/state", nil, &state, "", ""); err != nil {
		return nil, err
	}
	turn := value(state.Run.TurnId)
	if state.SessionId != scope.session || !value(state.Run.Active) || turn == "" {
		return nil, unauthorized
	}
	nativeName := privateTasksService + "__" + name
	for {
		s.mu.Lock()
		if !s.privateMPCCurrentLocked(scope) || proof.Err() != nil {
			s.mu.Unlock()
			return nil, unauthorized
		}
		var match reviewFact
		if view := s.state.Views[scope.session]; view != nil {
			for index, reviews := range []map[string]reviewFact{view.Reviews, view.LiveReviews, s.state.ManualMCPApprovals} {
				for _, review := range reviews {
					if review.Status != "approved" || review.TurnID != turn || index < 2 && review.ItemID == "" || review.ApprovalID == "" || review.ToolCallID == "" ||
						s.state.PrivateMCPClaims[review.ID] == turn || !reviewedMCPAction(review, nativeName, input) {
						continue
					}
					if match.ID != "" && match.ID != review.ID {
						s.mu.Unlock()
						return nil, unauthorized // Identical concurrent actions have no unique proof.
					}
					match = review
				}
			}
		}
		if match.ID != "" {
			prior := s.state.PrivateMCPClaims
			claims := map[string]string{match.ID: turn}
			for id, claimedTurn := range prior {
				if claimedTurn == turn {
					claims[id] = claimedTurn
				}
			}
			s.state.PrivateMCPClaims = claims
			if err := s.saveLocked(); err != nil {
				s.state.PrivateMCPClaims = prior
				s.mu.Unlock()
				return nil, err
			}
			s.mu.Unlock()
			invocation := api.ToolInvocation{Provider: "caelis", Session: scope.session, Turn: turn, TurnKey: turn,
				CallID: "bot-mcp-" + digest([]byte(scope.instance+"\x00"+scope.session+"\x00"+match.ApprovalID+"\x00"+match.ToolCallID))}
			return api.WithToolInvocation(context.WithValue(ctx, privateInvocationKey{}, scope), invocation), nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-proof.Done():
			return nil, unauthorized
		}
	}
}

// Manual decisions are acknowledged by Control before being spendable.
// The original head supplies the exact tool, raw arguments and call ID.
func (s *Session) recordManualMCPApproval(sid string, head *wire.ActiveApproval, permission nativePermission) error {
	if head.Subject == nil || head.Subject.Tool.State != wire.ApprovalFieldStateKnown || head.Target == nil || permission.ToolCall.ID == "" {
		return nil
	}
	tool := value(head.Subject.Tool.Text)
	if !slices.ContainsFunc(privateTasksTools, func(name string) bool { return tool == privateTasksService+"__"+name }) {
		return nil
	}
	action, err := json.Marshal(struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}{tool, permission.ToolCall.RawInput})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Session.SessionId != sid || !s.privateMPCCurrentLocked(s.privateMCP) {
		return nil
	}
	turn := head.Target.TurnId
	id := reviewID(sid, turn, head.RequestId)
	before := s.state.ManualMCPApprovals
	decisions := map[string]reviewFact{}
	for key, fact := range before {
		if fact.TurnID == turn {
			decisions[key] = fact
		}
	}
	decisions[id] = reviewFact{Review: api.Review{ID: id, Status: "approved", Action: string(action)}, TurnID: turn, ApprovalID: head.RequestId, ToolCallID: permission.ToolCall.ID}
	s.state.ManualMCPApprovals = decisions
	if err := s.saveLocked(); err != nil {
		s.state.ManualMCPApprovals = before
		return err
	}
	s.bumpLocked()
	return nil
}

func reviewedMCPAction(review reviewFact, tool string, args map[string]any) bool {
	var action struct {
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}
	return json.Unmarshal([]byte(review.Action), &action) == nil && action.Tool == tool && reflect.DeepEqual(action.Arguments, args)
}
