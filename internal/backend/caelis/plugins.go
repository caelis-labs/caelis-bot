package caelis

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

const atomicCapabilities = "application-atomic-capabilities-v1"

func corePlugins(selection plugins.Selection, binary string, builtins []string) ([]wire.ApplicationMCPServer, []string, []plugins.Issue) {
	servers := []wire.ApplicationMCPServer{}
	roots := append([]string{}, builtins...)
	roots = append(roots, selection.SkillRoots...)
	issues := append([]plugins.Issue{}, selection.Issues...)
	for _, p := range selection.Servers {
		name := plugins.RuntimeName(p.PackageID, p.Name)
		s := p.Server
		switch s.Type {
		case "stdio":
			if binary == "" {
				issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: "Bot executable unavailable"})
				continue
			}
			store := filepath.Dir(filepath.Dir(p.Data))
			servers = append(servers, wire.ApplicationMCPServer{Name: name, Transport: "stdio", Command: &binary, Args: []string{"--plugin-mcp", store, p.PackageID, p.Name, filepath.Base(p.Root)}, WorkDir: &p.Root})
		case "streamable-http", "sse":
			if len(s.Headers) > 0 {
				issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: "Remote headers need a Bot local connection proxy"})
				continue
			}
			transport := strings.ReplaceAll(s.Type, "-", "_")
			servers = append(servers, wire.ApplicationMCPServer{Name: name, Transport: transport, Url: &s.URL})
		}
	}
	slices.Sort(roots)
	return servers, roots, issues
}

func (s *Session) UpdateBotPlugins(ctx context.Context, selection plugins.Selection) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if s.tools == nil {
		s.mu.Unlock()
		return errors.New("Bot tools not configured")
	}
	binary := s.tools.Command
	builtins := append([]string(nil), s.tools.BuiltinSkillRoots...)
	sid := s.state.Session.SessionId
	connected := s.connected
	capable := slices.Contains(s.info.Capabilities, atomicCapabilities)
	for _, record := range s.state.Typed {
		if record.Outcome == "unknown" && strings.HasSuffix(record.Path, "/configuration") {
			s.mu.Unlock()
			return errors.New("original plugin configuration update is unresolved; reconnect and read its receipt")
		}
	}
	s.mu.Unlock()
	servers, roots, _ := corePlugins(selection, binary, builtins)
	if connected && !capable && (len(servers) > 0 || len(roots) > 0) {
		return errors.New("Caelis Core requires application-atomic-capabilities-v1")
	}
	if connected && sid != "" {
		current, err := s.configuration(ctx, sid)
		if err != nil {
			return err
		}
		operation := "plugin-" + digest([]byte(sid+"/"+string(current.Revision)+"/"+fmt.Sprint(selection.Revision)))
		_, err = s.updateConfiguration(ctx, sid, operation, string(current.Revision), map[string]any{"mcp_servers": servers, "skill_roots": roots, "skill_dirs": []string{}})
		if err != nil {
			var remote *remoteError
			if !errors.As(err, &remote) || remote.Status >= 500 {
				s.mu.Lock()
				s.connected = false
				s.issue = "Plugin configuration receipt unresolved; reconnect before new work"
				s.bumpLocked()
				s.mu.Unlock()
			}
			return err
		}
	}
	s.mu.Lock()
	if s.tools != nil {
		s.tools.Plugins = selection.Clone()
	}
	s.profile.McpServers = servers
	s.profile.SkillRoots = roots
	s.profile.SkillDirs = []string{}
	s.mu.Unlock()
	return nil
}

func (s *Session) BotPluginHealth(ctx context.Context) []plugins.Issue {
	s.mu.Lock()
	selection := plugins.Selection{}
	if s.tools != nil {
		selection = s.tools.Plugins.Clone()
	}
	c := s.client
	sid := s.state.Session.SessionId
	capable := slices.Contains(s.info.Capabilities, atomicCapabilities)
	s.mu.Unlock()
	_, _, issues := corePlugins(selection, s.profileBinary(), s.builtinSkillRoots())
	if !capable || c == nil || sid == "" {
		return issues
	}
	var status wire.ApplicationMCPStatus
	if err := c.json(ctx, "GET", "/application/sessions/"+idPath(sid)+"/mcp-status", nil, &status, "", ""); err != nil {
		return append(issues, plugins.Issue{Component: "runtime", Message: err.Error()})
	}
	for _, server := range status.Servers {
		if server.Status == "failed" {
			message := "MCP failed"
			if server.Warning != nil {
				message = *server.Warning
			}
			issues = append(issues, plugins.Issue{Component: "server", Name: server.Name, Message: message})
		}
	}
	for _, skill := range status.Skills {
		if skill.Status == "failed" {
			message := "Skill failed"
			if skill.Warning != nil {
				message = *skill.Warning
			}
			issues = append(issues, plugins.Issue{Component: "skill", Name: skill.Path, Message: message})
		}
	}
	return issues
}
func (s *Session) profileBinary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tools == nil {
		return ""
	}
	return s.tools.Command
}
func (s *Session) builtinSkillRoots() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tools == nil {
		return nil
	}
	return append([]string(nil), s.tools.BuiltinSkillRoots...)
}
