package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	profile := wire.ApplicationProfile{Version: "caelis-bot-application-v1", Execution: s.executionMode, Instructions: c.Instructions, Tools: []wire.ApplicationToolDefinition{}}
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
			definition := wire.ApplicationToolDefinition{Name: d.Name, Description: d.Description, InputSchema: schema, ApprovalPolicy: &policy}
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
		profile.Workspace = &wire.ApplicationWorkspace{Cwd: &c.NotebookDirectory}
	}
	profile.Permissions = &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}
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
	if provider, ok := c.Host.(api.LegacyToolProvider); ok {
		legacy := provider.LegacyToolConnection()
		if legacy != nil && legacy.Host != nil {
			definitions := []wire.ApplicationToolDefinition{}
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
				item := wire.ApplicationToolDefinition{Name: d.Name, Description: d.Description, InputSchema: schema, ApprovalPolicy: &policy}
				if d.ResultFormat != "" {
					item.ResultFormat = pointer(d.ResultFormat)
					content[d.Name] = true
				}
				definitions = append(definitions, item)
				handlers[d.Name] = legacy.Host
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

// Optional content-v1 tools require a positive handshake before any application
// registration or session mutation. Ordinary text-only Bot sessions still work
// with the existing required capability baseline.
func (s *Session) checkContentCapability(info wire.ServerInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tool := range s.profile.Tools {
		if value(tool.ResultFormat) == "content-v1" && !slices.Contains(info.Capabilities, "application-tool-result-content-v1") {
			return errors.New("Bot tools require Caelis application-tool-result-content-v1; update and restart the Host")
		}
	}
	if (len(s.profile.McpServers) > 0 || len(s.profile.SkillRoots) > 0) && !slices.Contains(info.Capabilities, atomicCapabilities) {
		return errors.New("Bot plugins require Caelis application-atomic-capabilities-v1")
	}
	return nil
}
func (s *Session) ensureSession(ctx context.Context, host *client) error {
	s.mu.Lock()
	b := s.state.Session
	op := s.state.CreateID
	c := s.client
	life := s.state.Connection
	s.mu.Unlock()
	if b.SessionId == "" {
		// Profile bytes are immutable. A restarted pending create must use the saved
		// journal and recover its operation, never regenerate a changed profile.
		if op != "" {
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
			// Deterministic operation from the connection: persisting CreateID then
			// command intent cannot generate a second session across a crash.
			op = "create-" + digest([]byte(life.ConnectionId))
			req := wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile}
			out, e := s.command(ctx, op, "/application/sessions", req)
			s.mu.Lock()
			s.state.CreateID = op
			save := s.saveLocked()
			s.mu.Unlock()
			if save != nil {
				return save
			}
			if e != nil {
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
	if e := c.json(ctx, "GET", "/application/sessions/"+idPath(b.SessionId), nil, &b, "", ""); e != nil {
		return e
	}
	if b.ApplicationId != life.ApplicationId || b.ConnectionId != life.ConnectionId || b.PrincipalId != life.PrincipalId || b.Archived || b.Profile.Execution != s.executionMode {
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
	if !pendingPlugin && (desired.Profile.ToolsVersion != s.profile.ToolsVersion || desired.Profile.Instructions != s.profile.Instructions || !slices.EqualFunc(desired.Profile.McpServers, s.profile.McpServers, func(a, b wire.ApplicationMCPServer) bool {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return string(left) == string(right)
	}) || !slices.Equal(desired.Profile.SkillRoots, s.profile.SkillRoots) || !slices.Equal(desired.Profile.SkillDirs, s.profile.SkillDirs)) {
		_, e = s.updateConfiguration(ctx, b.SessionId, "rebind-"+digest([]byte(string(desired.Revision)+s.profile.ToolsVersion+s.profile.Instructions+fmt.Sprint(s.profile.McpServers, s.profile.SkillRoots, s.profile.SkillDirs))), string(desired.Revision), map[string]any{"instructions": s.profile.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools, "mcp_servers": s.profile.McpServers, "skill_roots": s.profile.SkillRoots, "skill_dirs": s.profile.SkillDirs})
	}
	return e
}

func (s *Session) SubmitReport(ctx context.Context, in api.Submission) (api.Receipt, error) {
	return s.submit(ctx, in, nil, "application_summary")
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.ReportSubmitter = (*Session)(nil)
