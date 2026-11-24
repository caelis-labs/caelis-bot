package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (*Session) ApplicationCapabilities() api.ApplicationCapabilities {
	return api.ApplicationCapabilities{NativeFiles: true, WorkerExecution: true, ScheduledActivation: true}
}
func (s *Session) ConfigureBotTools(c *api.ToolConnection) error {
	s.step.Lock()
	defer s.step.Unlock()
	if c == nil {
		return errors.New("应用工具绑定无效或已固定")
	}
	profile := wire.SessionProfile{Version: "caelis-bot-application-v1", Execution: s.executionMode, Instructions: c.Instructions, Tools: []wire.SessionToolDefinition{}}
	if c.RuntimeVersion != "" {
		profile.Version += "/" + c.RuntimeVersion
	}
	// The native host already restored its user environment once. Select public
	// inheritance/non-login semantics without persisting a copy of environment
	// values or treating the Notebook CWD as HOME. Workers use the same Core defaults.
	if s.executionMode == "workspace-write" {
		profile.ExecutionConfig = &wire.ExecutionConfig{
			Environment: &wire.EnvironmentConfig{Inherit: pointer(true)},
			Shell:       &wire.ShellConfig{Login: pointer(false)},
		}
	}
	profile.ToolsVersion = digest([]byte("[]"))
	if c.NotebookDirectory != "" {
		profile.Workspace = &wire.SessionWorkspace{Cwd: &c.NotebookDirectory}
	}
	profile.Permissions = &wire.SessionPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}
	// Core discovers project MCP from the Session CWD under native workspace
	// trust. Only Bot-managed packages belong in the explicit profile.
	profile.McpServers, profile.SkillRoots, _ = corePlugins(c.Plugins, c.Command, c.BuiltinSkillRoots)
	residentMCP := c.PublishCaelisMCP != nil && !s.retainedWorkers
	if residentMCP {
		if c.Command == "" {
			return errors.New("Bot resident MCP executable is unavailable")
		}
		path, err := c.PublishCaelisMCP(c.NotebookDirectory)
		if err != nil {
			return err
		}
		profile.McpServers = append(profile.McpServers, wire.SessionMCPServer{
			Name: privateTasksService, Transport: "stdio", Command: pointer(c.Command),
			Args: append(slices.Clone(c.Args), path), WorkDir: pointer(c.NotebookDirectory),
		})
	}
	skillDirs, err := notebookSkillDirs(c.NotebookDirectory)
	if err != nil {
		return err
	}
	profile.SkillDirs = skillDirs
	s.mu.Lock()
	s.tools = c.Clone()
	s.profile = profile
	s.mu.Unlock()
	return nil
}

func (s *Session) checkCapabilityAssembly(info wire.ServerInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (len(s.profile.McpServers) > 0 || len(s.profile.SkillRoots) > 0 || len(s.profile.SkillDirs) > 0) && !slices.Contains(info.Capabilities, atomicCapabilities) {
		return errors.New("Bot plugins require Caelis session-atomic-capabilities-v1")
	}
	return nil
}
func (s *Session) ensureSession(ctx context.Context, host *client) error {
	s.mu.Lock()
	b := s.state.Session
	op := s.state.CreateID
	_, recordedCreate := s.state.Operations[op]
	c := s.client
	principal := s.state.PrincipalID
	s.mu.Unlock()
	if b.SessionId == "" {
		// Profile bytes are immutable. A restarted pending create must use the saved
		// journal and recover its operation, never regenerate a changed profile.
		if op != "" && recordedCreate {
			if e := s.recoverOperations(ctx); e != nil {
				return e
			}
			s.mu.Lock()
			j := s.state.Operations[op]
			s.mu.Unlock()
			if !succeeded(wire.Outcome(j.Outcome)) || j.Resource == "" {
				return errors.New("Caelis 对话创建结果未确认，已保留原请求")
			}
			b.SessionId = j.Resource
		} else {
			profile := s.profile
			profile.Model = s.execution.Model
			profile.ReasoningEffort = &s.execution.Effort
			profile.ServiceTier = &s.execution.ServiceTier
			if profile.Model == "" {
				models, e := setupCatalog(ctx, host, "model")
				if e != nil {
					return e
				}
				for _, m := range models {
					if m.Current && !m.NoAuth {
						profile.Model = m.Value
						break
					}
				}
				if profile.Model == "" {
					for _, m := range models {
						if !m.NoAuth {
							profile.Model = m.Value
							break
						}
					}
				}
			}
			if profile.Model == "" {
				return errors.New("请先连接并选择一个 Caelis 模型")
			}
			s.configureApproval(&profile)
			// Persist identity before dispatch, independently of Host generations.
			if op == "" {
				op = "create-" + rand.Text()
			}
			s.mu.Lock()
			s.state.CreateID = op
			s.mu.Unlock()
			req := wire.CreateSessionRequest{OperationId: &op, Profile: &profile}
			out, e := s.command(ctx, op, "/sessions", req)
			if e != nil {
				s.mu.Lock()
				if _, persisted := s.state.Operations[op]; !persisted {
					s.state.CreateID = ""
				}
				s.mu.Unlock()
				return e
			}
			if !succeeded(out.Outcome) || (value(out.SessionId) == "" && (out.Resource == nil || value(out.Resource.Ref) == "")) {
				return errors.New("Caelis 尚未确认对话创建")
			}
			b.SessionId = value(out.SessionId)
			if b.SessionId == "" {
				b.SessionId = value(out.Resource.Ref)
			}
		}
	}
	sid := b.SessionId
	if e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/profile", nil, &b, "", ""); e != nil {
		return e
	}
	if b.SessionId != sid || b.PrincipalId != principal || b.Archived || b.Profile.Execution != s.executionMode {
		return errors.New("Caelis 对话绑定不匹配或已归档")
	}
	s.mu.Lock()
	s.state.Session = b
	e := s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return e
	}
	if s.retainedWorkers {
		return nil
	}
	if e := s.checkReviewer(ctx, b); e != nil {
		return e
	}
	desired, e := s.configuration(ctx, b.SessionId)
	if e != nil {
		return e
	}
	// Read current configuration, not immutable creation profile. Rebind only the
	// application-owned instructions/catalog; preserve user model changes.
	s.mu.Lock()
	pendingPlugin := s.pluginConfigurationUnknownLocked()
	pendingConfiguration := s.configurationRecoveryPendingLocked()
	s.mu.Unlock()
	if pendingConfiguration && s.tools != nil && s.tools.PublishCaelisMCP != nil {
		if err := s.recoverConfigurations(ctx); err != nil {
			return err
		}
		desired, e = s.configuration(ctx, b.SessionId)
		if e != nil {
			return e
		}
		pendingPlugin = false
	}
	// An already running legacy Turn holds its original callback catalog.
	// Core owns cancellation and the unknown outcome of any claimed effect.
	// Retire that exact Turn before clearing the catalog; never claim or replay
	// it, and never cancel a Worker or an ordinary native MCP Turn.
	if len(desired.Profile.Tools) != 0 {
		if e := s.retireCallbackTurn(ctx, b.SessionId, c); e != nil {
			return e
		}
	}
	profile := s.profile
	servers := profile.McpServers
	if !pendingPlugin && (len(desired.Profile.Tools) != 0 || desired.Profile.ToolsVersion != profile.ToolsVersion || desired.Profile.Instructions != profile.Instructions ||
		!slices.EqualFunc(desired.Profile.McpServers, servers, sameMCPServer) || !slices.Equal(desired.Profile.SkillRoots, profile.SkillRoots) ||
		!slices.Equal(desired.Profile.SkillDirs, profile.SkillDirs)) {
		patch := map[string]any{"instructions": profile.Instructions, "tools_version": profile.ToolsVersion, "tools": profile.Tools, "mcp_servers": servers, "skill_roots": profile.SkillRoots, "skill_dirs": profile.SkillDirs}
		// Operation IDs share the Host principal namespace. Include the Session
		// and the exact structured intent; never rewrite retained receipt IDs.
		intent, err := json.Marshal(struct {
			SessionID string         `json:"session_id"`
			Revision  string         `json:"revision"`
			Patch     map[string]any `json:"patch"`
		}{b.SessionId, string(desired.Revision), patch})
		if err != nil {
			return err
		}
		_, e = s.updateConfiguration(ctx, b.SessionId, "rebind-"+digest(intent), string(desired.Revision), patch)
	}
	if e != nil {
		return e
	}
	return nil
}

func (s *Session) retireCallbackTurn(ctx context.Context, sid string, c *client) error {
	path := "/sessions/" + idPath(sid)
	var head wire.SessionState
	if err := c.json(ctx, "GET", path+"/state", nil, &head, "", ""); err != nil {
		return err
	}
	if head.SessionId != sid {
		return errors.New("旧会话升级目标不匹配")
	}
	if !value(head.Run.Active) {
		return nil
	}
	target := wire.TurnTarget{HandleId: value(head.Run.HandleId), RunId: value(head.Run.RunId), TurnId: value(head.Run.TurnId)}
	if target.TurnId == "" {
		return errors.New("旧会话尚未确认运行目标")
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	op := "retire-callback-turn-" + digest([]byte(sid+"\x00"+string(raw)))
	receipt, err := s.command(ctx, op, path+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	if err != nil {
		return err
	}
	if !succeeded(receipt.Outcome) {
		// The original Turn may have finished between the read and cancel.
		var latest wire.SessionState
		if err := c.json(ctx, "GET", path+"/state", nil, &latest, "", ""); err != nil {
			return err
		}
		if latest.SessionId != sid || value(latest.Run.Active) {
			return errors.New("旧会话升级尚未确认")
		}
	}
	return nil
}

func (s *Session) SubmitSystem(ctx context.Context, message api.SystemMessage) (api.Receipt, error) {
	if !message.Valid() {
		return api.Receipt{ID: message.ID, Outcome: "rejected"}, errors.New("invalid system message")
	}
	in := message.NativeInput()
	s.mu.Lock()
	original, exists := s.state.Operations[message.ID]
	prefix := s.state.ContextInputs[message.ID]
	s.mu.Unlock()
	if exists && (original.hostNotice() || original.Scheduled) {
		// A retained native intent may predate the shared marker. Inspect its
		// original bytes without rewriting, relabelling, or re-posting that intent.
		var prompt wire.PromptRequest
		if len(original.Body) != 0 {
			if json.Unmarshal(original.Body, &prompt) != nil || prompt.Input == nil || prefix < 0 || prefix > len(*prompt.Input) {
				return api.Receipt{ID: message.ID, Outcome: "unknown"}, errors.New("original system input unavailable")
			}
			body := (*prompt.Input)[prefix:]
			if body != in.Text && body != message.Text {
				return api.Receipt{ID: message.ID, Outcome: "rejected", Message: "同一标识不能用于不同消息"}, nil
			}
		}
		return api.Receipt{ID: message.ID, Outcome: productOutcome(wire.Outcome(original.Outcome))}, nil
	}
	// Scheduled is presentation only. Only arrangements use existing grants.
	if message.Source == api.SystemReminder || message.Source == api.SystemCallback {
		return s.submitScheduledSystem(ctx, in, message.Grants)
	}
	return s.submit(ctx, in, nil, "external_material")
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.SystemDelivery = (*Session)(nil)
