package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

const projectMarker = "# Managed by Caelis Bot. Bot workspace only.\n"

var projectLinkName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func ensureProjectDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Bot project directory is not a real directory")
	}
	return nil
}

func safeProjectLink(name string) bool {
	return projectLinkName.MatchString(name) && name != "." && name != ".." && filepath.Base(name) == name
}

func codexPluginConfigs(selection plugins.Selection, binary string) (map[string]map[string]any, []plugins.Issue) {
	out := map[string]map[string]any{}
	issues := append([]plugins.Issue{}, selection.Issues...)
	for _, item := range selection.Servers {
		name := plugins.RuntimeName(item.PackageID, item.Name)
		s := item.Server
		if item.Connection != nil || s.Type == "streamable-http" && len(s.Headers) > 0 {
			if binary == "" {
				issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: "Bot executable unavailable"})
				continue
			}
			store := filepath.Dir(filepath.Dir(item.Data))
			args := []string{"--plugin-mcp", store, item.PackageID, item.Name, filepath.Base(item.Root)}
			if item.Connection != nil {
				args = append(args, strconv.FormatUint(item.ConnectionRevision, 10))
			}
			out[name] = map[string]any{"command": binary, "args": args, "cwd": item.Root, "startup_timeout_sec": 10, "tool_timeout_sec": 30}
			continue
		}
		switch s.Type {
		case "stdio":
			resolved, err := s.Resolve(item.Root, item.Data)
			if err != nil {
				issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: err.Error()})
				continue
			}
			out[name] = map[string]any{"command": resolved.Command, "args": resolved.Args, "cwd": resolved.CWD, "env": resolved.Env, "startup_timeout_sec": 10, "tool_timeout_sec": 30}
		case "streamable-http":
			if len(s.Headers) > 0 {
				issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: "Remote headers need a Bot local connection proxy"})
				continue
			}
			out[name] = map[string]any{"url": s.URL, "startup_timeout_sec": 10, "tool_timeout_sec": 30}
		default:
			issues = append(issues, plugins.Issue{Component: "server", Name: name, Message: "Codex transport not supported by this adapter"})
		}
	}
	return out, issues
}

func projectCodexWorkspace(directory string, c *api.ToolConnection) error {
	if directory == "" {
		return nil
	}
	if !filepath.IsAbs(directory) {
		return errors.New("Bot workspace must be absolute")
	}
	for _, item := range c.Plugins.Servers {
		if item.Server.Type == "stdio" {
			if err := os.MkdirAll(item.Data, 0700); err != nil {
				return err
			}
		}
	}
	configDir := filepath.Join(directory, ".codex")
	skillDir := filepath.Join(directory, ".agents", "skills")
	if err := ensureProjectDir(directory); err != nil {
		return err
	}
	if err := ensureProjectDir(configDir); err != nil {
		return err
	}
	if err := ensureProjectDir(filepath.Join(directory, ".agents")); err != nil {
		return err
	}
	if err := ensureProjectDir(skillDir); err != nil {
		return err
	}
	configPath := filepath.Join(configDir, "config.toml")
	if body, err := os.ReadFile(configPath); err == nil && !strings.HasPrefix(string(body), projectMarker) {
		return errors.New("Bot project config is not owned by Bot")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	configs, _ := codexPluginConfigs(c.Plugins, c.Command)
	names := make([]string, 0, len(configs))
	for n := range configs {
		names = append(names, n)
	}
	sort.Strings(names)
	var body strings.Builder
	body.WriteString(projectMarker)
	for _, name := range names {
		item := configs[name]
		body.WriteString("\n[mcp_servers." + name + "]\n")
		for _, field := range []string{"command", "url", "cwd"} {
			if v, ok := item[field].(string); ok {
				body.WriteString(field + " = " + strconv.Quote(v) + "\n")
			}
		}
		if args, ok := item["args"].([]string); ok {
			if args == nil {
				args = []string{}
			}
			encoded, _ := json.Marshal(args)
			body.WriteString("args = " + string(encoded) + "\n")
		}
		if env, ok := item["env"].(map[string]string); ok {
			keys := make([]string, 0, len(env))
			for k := range env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			body.WriteString("env = { ")
			for i, k := range keys {
				if i > 0 {
					body.WriteString(", ")
				}
				body.WriteString(strconv.Quote(k) + " = " + strconv.Quote(env[k]))
			}
			body.WriteString(" }\n")
		}
		body.WriteString("startup_timeout_sec = 10\ntool_timeout_sec = 30\n")
	}
	// Stable links put metadata in the Bot cwd without copying package bodies.
	desired := map[string]string{}
	for _, root := range c.BuiltinSkillRoots {
		desired[filepath.Base(root)] = root
	}
	for _, root := range c.Plugins.SkillRoots {
		desired["plugin-"+filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(root))))+"-"+filepath.Base(root)] = root
	}
	for name, target := range desired {
		if !safeProjectLink(name) || !filepath.IsAbs(target) {
			return errors.New("unsafe Bot Skill link")
		}
	}
	manifestPath := filepath.Join(skillDir, ".caelis-bot-links.json")
	previous := map[string]string{}
	if raw, err := os.ReadFile(manifestPath); err == nil {
		if json.Unmarshal(raw, &previous) != nil {
			return errors.New("invalid Bot Skill link manifest")
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for name, target := range previous {
		if !safeProjectLink(name) || !filepath.IsAbs(target) {
			return errors.New("unsafe Bot Skill link manifest")
		}
	}
	// Resolve collisions before replacing the MCP config. A failed Skill
	// projection must leave the last confirmed config intact.
	for name, target := range previous {
		if desired[name] == target {
			continue
		}
		actual, err := os.Readlink(filepath.Join(skillDir, name))
		if err != nil || actual != target {
			return errors.New("Bot skill link was changed externally")
		}
	}
	for name, target := range desired {
		link := filepath.Join(skillDir, name)
		if actual, err := os.Readlink(link); err == nil {
			if actual != target {
				return errors.New("Bot skill link collision")
			}
			continue
		}
		if _, err := os.Lstat(link); err == nil {
			return errors.New("Bot skill path collision")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := writeProjectFile(configPath, []byte(body.String())); err != nil {
		return err
	}
	for name, target := range previous {
		if desired[name] == target {
			continue
		}
		link := filepath.Join(skillDir, name)
		actual, err := os.Readlink(link)
		if err != nil || actual != target {
			return errors.New("Bot skill link was changed externally")
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	for name, target := range desired {
		link := filepath.Join(skillDir, name)
		if actual, err := os.Readlink(link); err == nil {
			if actual != target {
				return errors.New("Bot skill link collision")
			}
			continue
		}
		if _, err := os.Lstat(link); err == nil {
			return errors.New("Bot skill path collision")
		}
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(desired)
	return writeProjectFile(manifestPath, raw)
}
func writeProjectFile(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".bot-project-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Session) UpdateBotPlugins(ctx context.Context, selection plugins.Selection) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.updateBotPluginsLocked(ctx, selection)
}

func (s *Session) WithBotPluginAdmission(ctx context.Context, mutate func(func(context.Context, plugins.Selection) error) error) error {
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return err
	}
	defer s.op.Unlock()
	return mutate(s.updateBotPluginsLocked)
}

func (s *Session) updateBotPluginsLocked(ctx context.Context, selection plugins.Selection) error {
	s.mu.Lock()
	if s.opts.BotTools == nil {
		s.mu.Unlock()
		return errors.New("Bot tools not configured")
	}
	// A disconnected resident may still own an unresolved native turn. Keep
	// its confirmed projection until reconnect reconciles the original thread.
	if s.client == nil && (s.binding.ThreadID != "" || s.binding.Pending != nil) {
		s.mu.Unlock()
		return errors.New("Bot is disconnected; reconnect and reconcile the original thread before changing plugins")
	}
	if s.binding.Pending != nil || s.state.Phase == "unknown" {
		s.mu.Unlock()
		return errors.New("original Bot request is unresolved; reconnect and read its receipt before changing active plugins")
	}
	old := s.opts.BotTools.Clone()
	next := old.Clone()
	next.Plugins = selection.Clone()
	c := s.client
	dir := s.opts.Directory
	s.mu.Unlock()
	rollback := func() error {
		var failures []error
		if err := projectCodexWorkspace(dir, old); err != nil {
			failures = append(failures, err)
		}
		if c != nil {
			recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := callDecode(recovery, c, "config/mcpServer/reload", nil, nil); err != nil {
				failures = append(failures, err)
			}
			if err := callDecode(recovery, c, "skills/list", map[string]any{"cwds": []string{dir}, "forceReload": true}, nil); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	fail := func(err error) error {
		if restoreErr := rollback(); restoreErr != nil {
			_ = s.connectionError("插件状态未确认，请重新连接后核对", restoreErr)
			return errors.Join(err, fmt.Errorf("plugin activation rollback failed: %w", restoreErr))
		}
		return err
	}
	if err := projectCodexWorkspace(dir, next); err != nil {
		return fail(err)
	}
	if c != nil {
		if err := callDecode(ctx, c, "config/mcpServer/reload", nil, nil); err != nil {
			return fail(err)
		}
		if err := callDecode(ctx, c, "skills/list", map[string]any{"cwds": []string{dir}, "forceReload": true}, nil); err != nil {
			return fail(err)
		}
	}
	s.mu.Lock()
	s.opts.BotTools = next
	s.mu.Unlock()
	return nil
}
func (s *Session) BotPluginHealth(ctx context.Context) []plugins.Issue {
	s.mu.Lock()
	selection := plugins.Selection{}
	binary := ""
	if s.opts.BotTools != nil {
		selection = s.opts.BotTools.Plugins.Clone()
		binary = s.opts.BotTools.Command
	}
	c := s.client
	dir := s.opts.Directory
	s.mu.Unlock()
	_, issues := codexPluginConfigs(selection, binary)
	if c == nil {
		return issues
	}
	var skills struct {
		Data []struct {
			Errors []struct{ Path, Message string } `json:"errors"`
		} `json:"data"`
	}
	if err := callDecode(ctx, c, "skills/list", map[string]any{"cwds": []string{dir}, "forceReload": false}, &skills); err != nil {
		issues = append(issues, plugins.Issue{Component: "runtime", Message: err.Error()})
	} else {
		for _, entry := range skills.Data {
			for _, failure := range entry.Errors {
				failurePath := failure.Path
				if canonical, err := filepath.EvalSymlinks(failurePath); err == nil {
					failurePath = canonical
				}
				for _, root := range selection.SkillRoots {
					rootPath := root
					if canonical, err := filepath.EvalSymlinks(rootPath); err == nil {
						rootPath = canonical
					}
					id := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(root))))
					link := "plugin-" + id + "-" + filepath.Base(root)
					if strings.HasPrefix(failurePath, rootPath+string(os.PathSeparator)) || failurePath == rootPath || strings.Contains(failure.Path, string(os.PathSeparator)+link+string(os.PathSeparator)) {
						issues = append(issues, plugins.Issue{Component: "skill", Name: root, Message: failure.Message})
					}
				}
			}
		}
	}
	// App-server status discovery can initialize an MCP connection. Keep the
	// settings snapshot cheap; the selected server is inspected on expansion.
	return issues
}

func (s *Session) BotPluginServer(ctx context.Context, name string) (plugins.ServerDetail, error) {
	empty := plugins.ServerDetail{State: "not_configured", Tools: []plugins.Tool{}}
	s.mu.Lock()
	selected := false
	if s.opts.BotTools != nil {
		for _, server := range s.opts.BotTools.Plugins.Servers {
			if plugins.RuntimeName(server.PackageID, server.Name) == name {
				selected = true
				break
			}
		}
	}
	c, thread := s.client, s.binding.ThreadID
	s.mu.Unlock()
	if !selected {
		return empty, nil
	}
	if c == nil || thread == "" {
		empty.State = "not_started"
		return empty, nil
	}
	var response struct {
		Data []struct {
			Name, RuntimeStatus, AuthStatus string
			ToolsError                      *string
			Tools                           map[string]struct {
				Name, Title, Description string
				Annotations              struct {
					ReadOnlyHint    *bool `json:"readOnlyHint"`
					DestructiveHint *bool `json:"destructiveHint"`
					IdempotentHint  *bool `json:"idempotentHint"`
					OpenWorldHint   *bool `json:"openWorldHint"`
				}
			}
		} `json:"data"`
	}
	if err := callDecode(ctx, c, "mcpServerStatus/list", map[string]any{"threadId": thread, "serverName": name, "detail": "toolsAndAuthOnly"}, &response); err != nil {
		return plugins.ServerDetail{State: "failed", Tools: []plugins.Tool{}}, err
	}
	for _, server := range response.Data {
		if server.Name != name {
			continue
		}
		out := plugins.ServerDetail{State: "unknown", Tools: []plugins.Tool{}}
		switch server.RuntimeStatus {
		case "connected":
			out.State = "connected"
		case "starting":
			out.State = "pending"
		case "notStarted":
			out.State = "not_started"
		case "authenticationRequired":
			out.State = "authentication_required"
		case "failed", "cancelled":
			out.State = "failed"
		case "disabled":
			out.State = "disabled"
		}
		if server.AuthStatus == "notLoggedIn" {
			out.State = "authentication_required"
		}
		if server.ToolsError != nil {
			out.State = "failed"
		}
		if out.State == "connected" {
			for key, tool := range server.Tools {
				toolName := plugins.SafeDisplayToolName(tool.Name)
				if toolName == "" {
					toolName = plugins.SafeDisplayToolName(key)
				}
				if toolName == "" {
					continue
				}
				out.Tools = append(out.Tools, plugins.Tool{Name: toolName, Title: plugins.SafeDisplayText(tool.Title), Description: plugins.SafeDisplayDescription(tool.Description), ReadOnlyHint: tool.Annotations.ReadOnlyHint, DestructiveHint: tool.Annotations.DestructiveHint, IdempotentHint: tool.Annotations.IdempotentHint, OpenWorldHint: tool.Annotations.OpenWorldHint})
			}
			sort.Slice(out.Tools, func(i, j int) bool { return out.Tools[i].Name < out.Tools[j].Name })
		}
		return out, nil
	}
	return empty, nil
}

func (s *Session) BotPluginGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return plugins.RuntimeDirectoryGeneration(s.epoch, s.binding.ThreadID)
}
