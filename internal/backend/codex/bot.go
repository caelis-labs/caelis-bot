package codex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Fixed across turns. Bot identity and trigger text never rewrite this prefix.
const botInstructions = `You are Caelis Bot, a persistent personal assistant living as a desktop companion. Talk naturally as in an IM conversation. There is no user-facing session, project, or workspace to create or select. Use your private working directory only for temporary inputs and results. Preserve continuity with the user; do not offer new conversations as recovery. Use native worker threads and their communication tools when useful, and summarize their results in your own reply. Do not expose worker IDs, protocol events, or routine tool narration. Ask for native approval when required; a pet action is never permission. Only user prompts and explicitly requested scheduled tasks activate work. For reminders use the caelis_bot tools, not shell sleep or OS scheduling. The app must remain running; hidden pets do not pause schedules, missed occurrences during sleep coalesce, and explicit quit pauses missed execution. Use bounded pet gestures for appropriate feedback; respect hidden state and reduced motion. Only claim a schedule or action succeeded after its tool receipt.`

// A source listing is not a per-tool catalog in Codex 0.153.4. Keep only names
// and purpose upfront; schemas and callable handles remain native tool_search data.
const botToolDiscovery = `
Caelis Bot provides the caelis_bot MCP source. Discover the needed tool before calling it: use tool_search when exposed; in Code Mode use the native ALL_TOOLS name/description lookup if tool_search is not exposed.
- bot_clock: read local time and scheduling availability.
- bot_reminders: list, create, update or remove user-requested resident reminders.
- bot_gesture: brief attention, nod or celebrate feedback on the desktop pet.
Use the discovered schema and native receipt; if discovery or execution fails, report that instead of claiming success.`

func (s *Session) connectionParams() map[string]any {
	instructions := botInstructions
	// The pinned native discovery flags are removed/no-op compatibility fields.
	// Native tool mode decides between tool_search and Code Mode metadata.
	config := map[string]any{}
	if s.opts.BotTools != nil {
		instructions += botToolDiscovery
		config["mcp_servers.caelis_bot"] = s.opts.BotTools
	}
	params := map[string]any{"runtimeWorkspaceRoots": []string{}, "developerInstructions": instructions, "cwd": s.opts.Directory, "sandbox": "workspace-write", "approvalPolicy": "on-request", "approvalsReviewer": "auto_review", "config": config}
	if s.opts.RequireApproval {
		params["approvalPolicy"] = "untrusted"
		params["approvalsReviewer"] = "user"
	}
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

// App Server need not stream every worker turn to the root subscription. Observe
// only an active worker, serially, and stop on its authoritative idle state.
const workerUnconfirmed = "后台工作的状态尚未确认，请重新连接核对"

func (s *Session) watchChild(c *Client, epoch uint64, id string) {
	if c == nil {
		return
	}
	for {
		s.mu.Lock()
		if s.client != c || s.epoch != epoch || s.closed {
			s.mu.Unlock()
			return
		}
		revision := s.childRevision[id]
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(s.life, 10*time.Second)
		var response struct {
			Thread nativeThread `json:"thread"`
		}
		err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &response)
		cancel()
		s.mu.Lock()
		if s.client != c || s.epoch != epoch || s.closed {
			s.mu.Unlock()
			return
		}
		if err != nil || response.Thread.ID != id {
			s.state.Message = workerUnconfirmed
			s.state.Phase = "unknown"
			s.update()
			delete(s.childWatching, id)
			s.mu.Unlock()
			return
		}
		done := false
		if revision == s.childRevision[id] {
			active := response.Thread.Status.Type == "active"
			run := ""
			for _, turn := range response.Thread.Turns {
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
				delete(s.childWatching, id)
				done = true
				if s.run == "" && len(s.childRuns) == 0 && len(s.prompts) == 0 && s.state.Phase == "working" {
					s.state.Phase = "completed"
				}
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
					if s.run == "" && len(s.childRuns) == 0 {
						s.state.Phase = "completed"
					} else {
						s.state.Phase = "working"
					}
				}
			}
			s.update()
		}
		s.mu.Unlock()
		if done {
			return
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-s.life.Done():
			timer.Stop()
			return
		case <-c.Done():
			timer.Stop()
			return
		}
	}
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
	s.childRevision[thread]++
	// Worker messages stay internal; only required decisions and lifecycle project
	// into the single Bot chat. IDs are scoped to their native target.
	var n struct {
		Turn      nativeTurn      `json:"turn"`
		RequestID json.RawMessage `json:"requestId"`
		Thread    nativeThread    `json:"thread"`
	}
	if json.Unmarshal(event.Params, &n) != nil {
		return
	}
	switch event.Method {
	case "turn/started", "turn/completed":
		if n.Turn.Status == "inProgress" {
			s.childRuns[thread] = n.Turn.ID
		}
		if terminal(n.Turn.Status) {
			if s.childRuns[thread] == n.Turn.ID {
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

func (s *Session) ConfigureBotTools(config map[string]any) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil || s.closed || s.closing {
		return errors.New("Bot 工具须在连接前配置")
	}
	s.opts.BotTools = config
	return nil
}
