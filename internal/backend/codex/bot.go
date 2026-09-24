package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func (s *Session) connectionParams() map[string]any {
	instructions := ""
	// The pinned native discovery flags are removed/no-op compatibility fields.
	// Native tool mode decides between tool_search and Code Mode metadata.
	config := map[string]any{}
	if s.opts.BotTools != nil {
		instructions = s.opts.BotTools.Instructions
		config["mcp_servers.caelis_bot"] = toolConfig(s.opts.BotTools)
		config["agents.enabled"] = false // Professional work goes through the owned task contract.
	}
	params := map[string]any{"runtimeWorkspaceRoots": []string{}, "developerInstructions": instructions, "cwd": s.opts.Directory, "sandbox": "workspace-write", "approvalPolicy": "on-request", "approvalsReviewer": "auto_review", "config": config}
	if s.opts.BotTools != nil && s.opts.BotTools.NotebookDirectory != "" {
		params["runtimeWorkspaceRoots"] = []string{s.opts.BotTools.NotebookDirectory}
	}
	if s.opts.RequireApproval {
		params["approvalPolicy"] = "untrusted"
		params["approvalsReviewer"] = "user"
	}
	s.applyExecution(params, true)
	return params
}

// V2 emits SubAgentActivity instead of v1 spawn/send collab items. Only native
// activity on an owned thread establishes ownership; text never does.
func (s *Session) trackWorkerActivity(item nativeItem) {
	id := item.AgentThreadID
	if item.Type != "subAgentActivity" || id == "" || id == s.binding.ThreadID {
		return
	}
	s.rememberChild(id)
	s.childRevision[id]++
	if item.ActivityKind == "completed" || item.ActivityKind == "interrupted" {
		// The activity has no turn id; read the child before declaring it idle.
	} else if item.ActivityKind != "started" && item.ActivityKind != "interacted" {
		return
	}
	if _, known := s.childRuns[id]; !known {
		s.childRuns[id] = ""
	}
	if s.childWatching[id] {
		return
	}
	s.childWatching[id] = true
	c, epoch := s.client, s.epoch
	go s.watchChild(c, epoch, id)
}

// One subscribe/reconciliation request per owned thread and connection. Native
// notifications remain subscribed across idle turns, including human TUI turns.
const workerUnconfirmed = "后台工作的状态尚未确认，请重新连接核对"

func (s *Session) watchChild(c *Client, epoch uint64, id string) {
	if c == nil {
		return
	}
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || s.closed {
		s.mu.Unlock()
		return
	}
	revision := s.childRevision[id]
	params := map[string]any{"threadId": id}
	if task := s.taskByThread(id); task != nil {
		params = s.workerParams(task.View.Workspace, task.Instructions, task)
		params["threadId"] = id
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.life, 10*time.Second)
	defer cancel()
	var response struct {
		Thread nativeThread `json:"thread"`
	}
	err := callDecode(ctx, c, "thread/resume", params, &response)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch || s.closed {
		return
	}
	if err != nil || response.Thread.ID != id {
		if err == nil {
			err = ErrProtocol
		}
		delete(s.childWatching, id)
		if task := s.taskByThread(id); task != nil {
			task.View.Status = "unknown"
			_ = s.save()
		} else {
			s.state.Phase, s.state.Message = "unknown", workerUnconfirmed
		}
		s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "worker_subscription_failed", Method: "thread/resume", Thread: id, Reason: diagnosticlog.Reason(err.Error()), Fingerprint: diagnosticlog.Fingerprint([]byte(err.Error()))})
		s.update()
		return
	}
	// Live notifications that arrived during resume supersede its snapshot.
	if revision != s.childRevision[id] {
		return
	}
	active := response.Thread.Status.Type == "active"
	run := ""
	for _, turn := range response.Thread.Turns {
		if task := s.taskByThread(id); task != nil {
			s.observeTaskTurn(task, turn)
		}
		if turn.Status == "inProgress" && !s.childTerminals[opaque(id, turn.ID)] {
			active = true
			run = turn.ID
		}
		if terminal(turn.Status) {
			s.childTerminals[opaque(id, turn.ID)] = true
			for key, p := range s.prompts {
				if p.thread == id && p.turn == turn.ID {
					s.resolvePrompt(key, p)
				}
			}
		}
	}
	if active {
		s.childRuns[id] = run
	} else {
		delete(s.childRuns, id)
	}
	if s.state.Message == workerUnconfirmed && s.binding.Pending == nil {
		unresolved := false
		for child := range s.childRuns {
			if !s.childWatching[child] {
				unresolved = true
			}
		}
		if !unresolved {
			s.state.Message = ""
			s.state.Phase = "working"
		}
	}
	if s.run == "" && len(s.childRuns) == 0 && len(s.prompts) == 0 && s.state.Phase == "working" {
		s.state.Phase = "completed"
	}
	_ = s.save()
	s.update()
}

func (s *Session) ownsThread(id string) bool {
	return id != "" && (id == s.binding.ThreadID || s.children[id])
}
func (s *Session) rememberChild(id string) {
	if id == "" || id == s.binding.ThreadID || s.children[id] {
		return
	}
	s.children[id] = true
	s.binding.Children = append(s.binding.Children, id)
	if s.save() != nil {
		s.state.Message = "后台工作记录保存失败，请保持应用运行并核对结果"
	}
}
func (s *Session) childEvent(event Notification, thread, turn string) {
	if event.Method == "error" {
		var n struct {
			Error turnError `json:"error"`
		}
		if s.decodeEvent(event, &n, false) {
			s.logEvent(event, "worker_error", diagnosticlog.Reason(n.Error.Message))
		}
		return // The worker's terminal fact, not an error notice, owns its outcome.
	}
	// Only consumed state can supersede reconciliation. Metadata such as token
	// usage or thread/started must not discard the only restored task snapshot.
	switch event.Method {
	case "item/completed", "turn/started", "turn/completed", "serverRequest/resolved", "thread/closed", "thread/deleted":
		s.childRevision[thread]++
	default:
		return
	}
	// Worker messages stay internal; only required decisions and lifecycle project
	// into the single Bot chat. IDs are scoped to their native target.
	var n struct {
		Turn      nativeTurn      `json:"turn"`
		RequestID json.RawMessage `json:"requestId"`
		Thread    nativeThread    `json:"thread"`
		Item      nativeItem      `json:"item"`
	}
	if !s.decodeEvent(event, &n, false) {
		if event.Method == "turn/started" || event.Method == "turn/completed" {
			if task := s.taskByThread(thread); task != nil {
				task.View.Status = "unknown"
			} else {
				s.state.Phase, s.state.Message = "unknown", workerUnconfirmed
			}
		}
		return
	}
	switch event.Method {
	case "item/completed":
		if task := s.taskByThread(thread); task != nil && turn == task.Run && n.Item.Type == "agentMessage" {
			task.View.Result = boundedText(n.Item.Text, 6000)
			_ = s.save()
		}
	case "turn/started", "turn/completed":
		if task := s.taskByThread(thread); task != nil {
			if s.observeTaskTurn(task, n.Turn) {
				_ = s.save()
			}
		}
		if n.Turn.Status == "inProgress" {
			s.childRuns[thread] = n.Turn.ID
		}
		if terminal(n.Turn.Status) {
			if current, known := s.childRuns[thread]; known && (current == "" || current == n.Turn.ID) {
				delete(s.childRuns, thread)
			}
			s.childTerminals[opaque(thread, n.Turn.ID)] = true
			for id, p := range s.prompts {
				if p.thread == thread && p.turn == n.Turn.ID {
					s.resolvePrompt(id, p)
				}
			}
		}
	case "serverRequest/resolved":
		id := s.promptHandles[string(n.RequestID)]
		if p := s.prompts[id]; p != nil && p.thread == thread {
			s.resolvePrompt(id, p)
		}
	case "thread/closed", "thread/deleted":
		delete(s.childRuns, thread)
		for id, p := range s.prompts {
			if p.thread == thread {
				s.resolvePrompt(id, p)
			}
		}
	}
	if s.run == "" && len(s.childRuns) == 0 && len(s.prompts) == 0 && s.state.Phase == "working" {
		s.state.Phase = "completed"
	}
}
func (s *Session) resolvePrompt(id string, p *prompt) {
	p.view.Status = "resolved"
	s.replacePrompt(id, p.view)
	delete(s.prompts, id)
	delete(s.promptHandles, string(p.id))
	if len(s.prompts) == 0 && s.state.Phase == "attention" {
		if s.run != "" || len(s.childRuns) > 0 {
			s.state.Phase = "working"
		} else {
			s.state.Phase = "idle"
		}
	}
}

// Compile-time contract: Bot is a single product interaction, even when Codex
// maintains several internal threads. No NewConversation command is exposed.
var _ api.Engine = (*Session)(nil)

func (s *Session) ConfigureBotTools(config *api.ToolConnection) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil || s.closed || s.closing {
		return errors.New("Bot 工具须在连接前配置")
	}
	if config == nil || config.Command == "" {
		return errors.New("Bot 工具连接无效")
	}
	if config.NotebookDirectory != "" {
		if !filepath.IsAbs(config.NotebookDirectory) {
			return errors.New("Notebook 目录必须是完整路径")
		}
		s.opts.Directory = config.NotebookDirectory
	}
	s.opts.BotTools = config.Clone()
	for _, t := range s.binding.Tasks {
		if t.Instructions == "" {
			t.Instructions = config.WorkerInstructions
		}
	}
	if len(s.binding.Tasks) > 0 {
		return s.save()
	}
	return nil
}

// Codex-specific MCP policy is projected here, never assembled by the Bot host.
func toolConfig(c *api.ToolConnection) map[string]any {
	approved := map[string]any{}
	for _, name := range c.ApprovedTools {
		approved[name] = map[string]string{"approval_mode": "approve"}
	}
	return map[string]any{"command": c.Command, "args": c.Args, "env": c.Env,
		"tools": approved, "startup_timeout_sec": 10, "tool_timeout_sec": 15}
}
