package caelis

import (
	"context"
	"encoding/json"
	"errors"
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
	profile := wire.ApplicationProfile{Version: "caelis-bot-application-v1", Execution: s.executionMode, Instructions: c.Instructions, Tools: []wire.ApplicationToolDefinition{}}
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
			profile.Tools = append(profile.Tools, wire.ApplicationToolDefinition{Name: d.Name, Description: d.Description, InputSchema: schema})
		}
	}
	sort.Slice(profile.Tools, func(i, j int) bool { return profile.Tools[i].Name < profile.Tools[j].Name })
	b, _ := json.Marshal(profile.Tools)
	profile.ToolsVersion = digest(b)
	if c.NotebookDirectory != "" {
		profile.Workspace = &wire.ApplicationWorkspace{Cwd: &c.NotebookDirectory}
	}
	profile.Permissions = &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}
	s.mu.Lock()
	if s.catalogs == nil {
		s.catalogs = map[string]map[string]api.ApplicationTools{}
	}
	s.catalogs[profile.ToolsVersion] = catalog
	s.catalog = catalog
	s.profile = profile
	s.mu.Unlock()
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
	desired, e := s.configuration(ctx, b.SessionId)
	if e != nil {
		return e
	}
	// Read current configuration, not immutable creation profile. Rebind only the
	// application-owned instructions/catalog; preserve user model changes.
	if desired.Profile.ToolsVersion != s.profile.ToolsVersion || desired.Profile.Instructions != s.profile.Instructions {
		_, e = s.updateConfiguration(ctx, b.SessionId, "rebind-"+digest([]byte(string(desired.Revision)+s.profile.ToolsVersion+s.profile.Instructions)), string(desired.Revision), map[string]any{"instructions": s.profile.Instructions, "tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools})
	}
	return e
}

func (s *Session) SubmitReport(ctx context.Context, in api.Submission) (api.Receipt, error) {
	return s.submit(ctx, in, nil, "application_summary")
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.ReportSubmitter = (*Session)(nil)
