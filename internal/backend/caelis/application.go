package caelis

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (*Session) ApplicationCapabilities() api.ApplicationCapabilities {
	return api.ApplicationCapabilities{NativeFiles: true, WorkerExecution: true, ScheduledActivation: true}
}
func (s *Session) ConfigureBotTools(c *api.ToolConnection) error {
	s.step.Lock()
	defer s.step.Unlock()
	if c == nil || c.Host == nil {
		return errors.New("应用工具绑定无效或已固定")
	}
	s.tools = c.Clone()
	catalog := map[string]api.ApplicationTools{}
	formats := map[string]bool{}
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
	for _, host := range []api.ApplicationTools{c.Host} {
		if host == nil {
			continue
		}
		for _, d := range host.Definitions() {
			if _, exists := catalog[d.Name]; exists {
				return errors.New("应用工具名称重复")
			}
			var schema wire.JSONObject
			if d.Name == "" || json.Unmarshal(d.InputSchema, &schema) != nil || schema == nil {
				return errors.New("应用工具 schema 无效")
			}
			catalog[d.Name] = host
			policy := "required"
			if slices.Contains(c.ApprovedTools, d.Name) {
				policy = "direct"
			}
			definition := wire.SessionToolDefinition{Name: d.Name, Description: d.Description, InputSchema: schema, ApprovalPolicy: &policy}
			if d.ResultFormat != "" {
				if d.ResultFormat != "content-v1" {
					return errors.New("unsupported application tool result format")
				}
				definition.ResultFormat = pointer(d.ResultFormat)
				formats[d.Name] = true
			}
			profile.Tools = append(profile.Tools, definition)
		}
	}
	sort.Slice(profile.Tools, func(i, j int) bool { return profile.Tools[i].Name < profile.Tools[j].Name })
	b, _ := json.Marshal(profile.Tools)
	profile.ToolsVersion = digest(b)
	if c.NotebookDirectory != "" {
		profile.Workspace = &wire.SessionWorkspace{Cwd: &c.NotebookDirectory}
	}
	profile.Permissions = &wire.SessionPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}
	profile.McpServers, profile.SkillRoots, _ = corePlugins(c.Plugins, c.Command, c.BuiltinSkillRoots)
	profile.SkillDirs = []string{}
	s.mu.Lock()
	if s.catalogs == nil {
		s.catalogs = map[string]map[string]api.ApplicationTools{}
	}
	if s.state.ContentCatalogs == nil {
		s.state.ContentCatalogs = map[string]map[string]bool{}
	}
	if len(formats) > 0 {
		s.state.ContentCatalogs[profile.ToolsVersion] = formats
		if err := s.saveLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	older := []*api.ToolConnection{}
	if provider, ok := c.Host.(api.LegacyToolProvider); ok {
		older = append(older, provider.LegacyToolConnection())
	}
	if provider, ok := c.Host.(api.HistoricalToolProvider); ok {
		older = append(older, provider.HistoricalToolConnections()...)
	}
	for _, legacy := range older {
		if legacy != nil && legacy.Host != nil {
			definitions := []wire.SessionToolDefinition{}
			handlers := map[string]api.ApplicationTools{}
			content := map[string]bool{}
			for _, d := range legacy.Host.Definitions() {
				var schema wire.JSONObject
				if json.Unmarshal(d.InputSchema, &schema) != nil || schema == nil {
					s.mu.Unlock()
					return errors.New("invalid legacy tool schema")
				}
				policy := "required"
				if slices.Contains(legacy.ApprovedTools, d.Name) {
					policy = "direct"
				}
				item := wire.SessionToolDefinition{Name: d.Name, Description: d.Description, InputSchema: schema, ApprovalPolicy: &policy}
				if d.ResultFormat != "" {
					item.ResultFormat = pointer(d.ResultFormat)
					content[d.Name] = true
				}
				definitions = append(definitions, item)
				if d.Name != "bot_care" && d.Name != "bot_care_read" {
					handlers[d.Name] = legacy.Host
				}
			}
			sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
			raw, _ := json.Marshal(definitions)
			version := digest(raw)
			s.catalogs[version] = handlers
			if len(content) > 0 {
				s.state.ContentCatalogs[version] = content
				if err := s.saveLocked(); err != nil {
					s.mu.Unlock()
					return err
				}
			}
		}
	}
	s.catalogs[profile.ToolsVersion] = catalog
	s.catalog = catalog
	s.profile = profile
	s.mu.Unlock()
	return nil
}

// Optional content-v1 tools require a positive handshake before any Session
// mutation. Ordinary text-only Bot sessions still work
// with the existing required capability baseline.
func (s *Session) checkContentCapability(info wire.ServerInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tool := range s.profile.Tools {
		if value(tool.ResultFormat) == "content-v1" && !slices.Contains(info.Capabilities, "session-tool-result-content-v1") {
			return errors.New("Bot tools require Caelis session-tool-result-content-v1; update and restart the Host")
		}
	}
	if (len(s.profile.McpServers) > 0 || len(s.profile.SkillRoots) > 0) && !slices.Contains(info.Capabilities, atomicCapabilities) {
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
			s.configureReviewer(&profile)
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
	if e := c.json(ctx, "GET", "/sessions/"+idPath(b.SessionId)+"/profile", nil, &b, "", ""); e != nil {
		return e
	}
	if b.PrincipalId != principal || b.Archived || b.Profile.Execution != s.executionMode {
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
	s.mu.Unlock()
	if !pendingPlugin && (desired.Profile.ToolsVersion != s.profile.ToolsVersion || desired.Profile.Instructions != s.profile.Instructions || !slices.EqualFunc(desired.Profile.McpServers, s.profile.McpServers, func(a, b wire.SessionMCPServer) bool {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return string(left) == string(right)
	}) || !slices.Equal(desired.Profile.SkillRoots, s.profile.SkillRoots) || !slices.Equal(desired.Profile.SkillDirs, s.profile.SkillDirs)) {
		patch := map[string]any{"instructions": s.profile.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools, "mcp_servers": s.profile.McpServers, "skill_roots": s.profile.SkillRoots, "skill_dirs": s.profile.SkillDirs}
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
	return e
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
	if in.Scheduled {
		return s.submitScheduledSystem(ctx, in, message.Grants)
	}
	return s.submit(ctx, in, nil, "external_material")
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.SystemDelivery = (*Session)(nil)
