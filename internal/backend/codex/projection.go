package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type nativeThread struct {
	ID             string       `json:"id"`
	ParentThreadID string       `json:"parentThreadId"`
	Turns          []nativeTurn `json:"turns"`
	Status         struct {
		Type string `json:"type"`
	} `json:"status"`
}
type nativeTurn struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Items  []nativeItem `json:"items"`
	Error  *turnError   `json:"error"`
}
type nativeInput struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	TextElements []any  `json:"text_elements,omitempty"`
	Path         string `json:"path,omitempty"`
	Name         string `json:"name,omitempty"`
}
type nativeChange struct {
	Path string          `json:"path"`
	Diff string          `json:"diff"`
	Kind json.RawMessage `json:"kind"`
}
type nativeItem struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Text              string          `json:"text"`
	ClientID          string          `json:"clientId"`
	Content           []nativeInput   `json:"content"`
	Status            string          `json:"status"`
	Phase             string          `json:"phase"`
	Command           string          `json:"command"`
	Cwd               string          `json:"cwd"`
	Output            string          `json:"aggregatedOutput"`
	ExitCode          *int            `json:"exitCode"`
	Changes           []nativeChange  `json:"changes"`
	Server            string          `json:"server"`
	Tool              string          `json:"tool"`
	Arguments         json.RawMessage `json:"arguments"`
	Result            json.RawMessage `json:"result"`
	Error             json.RawMessage `json:"error"`
	Path              string          `json:"path"`
	AgentThreadID     string          `json:"agentThreadId"`
	ActivityKind      string          `json:"kind"`
	ReceiverThreadIDs []string        `json:"receiverThreadIds"`
	Questions         []struct {
		Title   string   `json:"title"`
		Options []string `json:"options"`
	} `json:"questions"`
}

func terminal(status string) bool {
	return status == "completed" || status == "failed" || status == "interrupted"
}
func (s *Session) applyTurn(turn nativeTurn, history bool) {
	if turn.ID == "" {
		return
	}
	if terminal(s.runs[turn.ID]) && !terminal(turn.Status) {
		return
	}
	s.runs[turn.ID] = turn.Status
	for _, item := range turn.Items {
		s.applyItem(turn.ID, item, terminal(turn.Status))
	}
	if turn.Status == "inProgress" {
		s.run = turn.ID
		s.state.Phase = "working"
	}
	if terminal(turn.Status) {
		if s.run == turn.ID {
			s.run = ""
		}
		if !history && s.run == "" {
			s.state.Phase = turn.Status
			if turn.Error != nil {
				s.applyFailure(*turn.Error)
			}
		}
		if turn.Status == "interrupted" || turn.Status == "failed" {
			// A turn terminal is authoritative, but not an individual tool receipt.
			for key, native := range s.nativeItems {
				if key == opaque(turn.ID, native.ID) && !terminal(native.Status) {
					if index, ok := s.items[key]; ok && s.state.Items[index].Kind == "activity" {
						s.state.Items[index].Status = "unconfirmed"
					}
				}
			}
		}
		for id, p := range s.prompts {
			if p.thread == s.binding.ThreadID && p.turn == turn.ID {
				p.view.Status = "resolved"
				s.replacePrompt(id, p.view)
				delete(s.prompts, id)
				delete(s.promptHandles, string(p.id))
			}
		}
	}
}
func (s *Session) applyItem(run string, item nativeItem, complete bool) {
	if item.ID == "" {
		return
	}
	key := opaque(run, item.ID)
	old, existed := s.nativeItems[key]
	if existed && terminal(old.Status) && !complete {
		return
	}
	if complete && item.Status == "" {
		item.Status = "completed"
	}
	s.nativeItems[key] = item
	view := api.Item{TurnKey: opaque(run), ID: key, Status: item.Status, Artifacts: []api.Artifact{}}
	switch item.Type {
	case "userMessage":
		view.Kind = "user"
		var text []string
		for _, part := range item.Content {
			if part.Type == "text" {
				value := part.Text
				if strings.HasPrefix(value, "[用户附件：") {
					value = strings.SplitN(value, "\n", 2)[0]
				}
				text = append(text, value)
			} else if part.Type == "skill" || part.Type == "mention" {
				text = append(text, "引用："+part.Name)
			} else if part.Path != "" {
				text = append(text, "附件："+filepath.Base(part.Path))
			}
		}
		view.Text = strings.Join(text, "\n")
		if p := s.binding.Pending; p != nil && p.ID == item.ClientID {
			s.state.LastReceipt = api.Receipt{ID: p.ID, Outcome: "accepted"}
			receipt := s.state.LastReceipt
			s.binding.LastReceipt = &receipt
			s.binding.Pending = nil
			if s.save() != nil {
				s.state.Message = "已收到发送回执，但本地记录保存失败"
			}
		}
	case "agentMessage":
		view.Kind = "assistant"
		view.Text = item.Text
		if complete {
			for _, match := range artifactLink.FindAllStringSubmatch(item.Text, -1) {
				path := strings.Trim(match[2], "<>")
				if u, err := url.Parse(path); err == nil && (u.Scheme != "" || strings.HasPrefix(path, "#")) {
					continue
				}
				if !filepath.IsAbs(path) {
					path = filepath.Join(s.opts.Directory, path)
				}
				rel, err := filepath.Rel(s.opts.Directory, filepath.Clean(path))
				if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !strings.HasPrefix(rel, ".attachments"+string(filepath.Separator)) {
					s.addArtifact(&view, path)
					view.Text = strings.ReplaceAll(view.Text, match[0], match[1])
				}
			}
		}
		for _, q := range item.Questions {
			view.Text += "\n\n" + q.Title
			for _, o := range q.Options {
				view.Text += "\n• " + o
			}
		}
	case "plan":
		view.Kind = "activity"
		view.Text = "计划"
		view.Details = item.Text
	case "commandExecution":
		view.Kind = "activity"
		view.Text = "运行命令"
		view.Details = item.Command + "\n位置：" + item.Cwd
		if complete {
			view.Details += "\n" + item.Output
			if item.ExitCode != nil {
				view.Details += fmt.Sprintf("\n退出码：%d", *item.ExitCode)
			}
		}
	case "fileChange":
		view.Kind = "activity"
		view.Text = "更新文件"
		for _, change := range item.Changes {
			view.Details += change.Path + "\n" + change.Diff + "\n"
			if complete && item.Status == "completed" {
				s.addArtifact(&view, change.Path)
			}
		}
	case "mcpToolCall", "dynamicToolCall":
		view.Kind = "activity"
		view.Text = "使用 " + item.Tool
		if item.Server != "" {
			view.Text = "使用 " + item.Server + " · " + item.Tool
		}
		view.Details = string(item.Arguments)
		if complete {
			view.Details += "\n" + string(item.Result)
			if len(item.Error) > 0 && string(item.Error) != "null" {
				view.Details += "\n" + string(item.Error)
			}
		}
	case "collabAgentToolCall":
		view.Kind = "activity"
		view.Text = "协作处理"
		view.Details = item.Tool
		for _, id := range item.ReceiverThreadIDs {
			s.rememberChild(id)
		}
	case "imageView", "imageGeneration":
		view.Kind = "activity"
		view.Text = "处理图片"
		if complete {
			s.addArtifact(&view, item.Path)
		}
	case "webSearch":
		view.Kind = "activity"
		view.Text = "搜索资料"
	case "contextCompaction":
		view.Kind = "activity"
		view.Text = "整理对话上下文"
	case "subAgentActivity":
		s.rememberChild(item.AgentThreadID)
		view.Kind = "activity"
		view.Text = "协作处理"
		view.Details = item.ActivityKind
	case "reasoning", "hookPrompt", "functionCallOutput":
		return
	default:
		view.Kind = "activity"
		view.Text = "处理任务"
	}
	if index, ok := s.items[key]; ok {
		s.state.Items[index] = view
	} else {
		s.items[key] = len(s.state.Items)
		s.state.Items = append(s.state.Items, view)
	}
}

var artifactLink = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\n]+)\)`)

func (s *Session) addArtifact(view *api.Item, path string) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.opts.Directory, path)
	}
	path = filepath.Clean(path)
	id := opaque("artifact", path)
	s.artifacts[id] = path
	view.Artifacts = append(view.Artifacts, api.Artifact{ID: id, Name: filepath.Base(path)})
}
func (s *Session) applyEvent(event Notification) {
	if len(event.RequestID) > 0 {
		s.addPrompt(event)
		return
	}
	// Select the native method before decoding its payload. Same-named fields
	// (notably error) have different types in unrelated notifications.
	switch event.Method {
	case "account/login/completed":
		var n struct {
			LoginID string `json:"loginId"`
			Success bool   `json:"success"`
		}
		if s.decodeEvent(event, &n, false) && n.LoginID != "" {
			if n.LoginID == s.loginID {
				s.loginCompleted(n.Success)
			} else if s.loginStarting {
				s.earlyLogin[n.LoginID] = n.Success
			}
		}
		return
	case "mcpServer/startupStatus/updated", "mcpServer/oauthLogin/completed", "windowsSandbox/setupCompleted":
		s.componentEvent(event)
		return
	case "thread/started":
		var n struct {
			Thread struct {
				ID     string `json:"id"`
				Parent string `json:"parentThreadId"`
			} `json:"thread"`
		}
		if s.decodeEvent(event, &n, false) && s.ownsThread(n.Thread.Parent) {
			s.rememberChild(n.Thread.ID)
		}
		return
	case "item/autoApprovalReview/started", "item/autoApprovalReview/completed":
		s.applyReview(event)
		return
	case "turn/started", "turn/completed", "item/started", "item/completed",
		"item/agentMessage/delta", "item/plan/delta", "item/commandExecution/outputDelta",
		"item/mcpToolCall/progress", "serverRequest/resolved", "error", "thread/closed", "thread/deleted", "account/updated":
		// Consumed below, with only this method's fields.
	case "thread/status/changed", "thread/tokenUsage/updated", "account/rateLimits/updated",
		"item/reasoning/textDelta", "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded",
		"turn/diff/updated", "turn/plan/updated", "skills/changed", "thread/name/updated":
		return // Native metadata has no Bot presentation or lifecycle effect.
	default:
		s.logEvent(event, "notification_ignored", "unconsumed notification; no execution state changed")
		return
	}
	var target struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
	}
	if !s.decodeEvent(event, &target, false) {
		return
	}
	if target.ThreadID != "" && target.ThreadID != s.binding.ThreadID {
		if s.children[target.ThreadID] {
			s.childEvent(event, target.ThreadID, target.TurnID)
		}
		return
	}
	switch event.Method {
	case "turn/started", "turn/completed":
		var n struct {
			Turn nativeTurn `json:"turn"`
		}
		if s.decodeEvent(event, &n, true) {
			if n.Turn.Error != nil {
				s.logEvent(event, "turn_failed", diagnosticlog.Reason(n.Turn.Error.Message))
			}
			s.applyTurn(n.Turn, false)
		}
	case "item/started", "item/completed":
		var n struct {
			Item nativeItem `json:"item"`
		}
		if s.decodeEvent(event, &n, false) {
			complete := event.Method == "item/completed"
			s.applyItem(target.TurnID, n.Item, complete)
			if complete {
				s.trackWorkerActivity(n.Item)
				if n.Item.Status == "failed" {
					s.logEvent(event, "tool_failed", "native tool failed; execution remains runtime-owned")
				}
			}
		}
	case "item/agentMessage/delta", "item/plan/delta", "item/commandExecution/outputDelta":
		var n struct {
			Delta string `json:"delta"`
		}
		if !s.decodeEvent(event, &n, false) {
			return
		}
		key := opaque(target.TurnID, target.ItemID)
		item := s.nativeItems[key]
		if terminal(item.Status) || terminal(s.runs[target.TurnID]) {
			return
		}
		if event.Method == "item/commandExecution/outputDelta" {
			if item.ID != "" {
				item.Output += n.Delta
				s.nativeItems[key] = item
			}
			return
		}
		item.ID, item.Type = target.ItemID, "agentMessage"
		if event.Method == "item/plan/delta" {
			item.Type = "plan"
		}
		item.Text += n.Delta
		s.applyItem(target.TurnID, item, false)
	case "item/mcpToolCall/progress":
		var n struct {
			Message string `json:"message"`
		}
		if s.decodeEvent(event, &n, false) {
			if index, ok := s.items[opaque(target.TurnID, target.ItemID)]; ok {
				s.state.Items[index].Details = n.Message
			}
		}
	case "serverRequest/resolved":
		var n struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if !s.decodeEvent(event, &n, true) {
			return
		}
		id := s.promptHandles[string(n.RequestID)]
		if p, ok := s.prompts[id]; ok {
			s.resolvePrompt(id, p)
		}
	case "error":
		var n struct {
			Error     turnError `json:"error"`
			WillRetry bool      `json:"willRetry"`
		}
		if !s.decodeEvent(event, &n, true) {
			return
		}
		s.logEvent(event, "turn_error", diagnosticlog.Reason(n.Error.Message))
		if n.WillRetry {
			return
		} // Native retry is not a request for user action.
		if n.Error.Message != "" {
			s.applyFailure(n.Error)
		}
		s.state.Phase = "failed"
	case "thread/closed", "thread/deleted":
		s.bound = false
		s.state.Connection = "offline"
		s.state.Phase = "unknown"
		s.state.Message = "对话已关闭，请重新连接"
	case "account/updated":
		if s.state.Connection == "login" {
			s.state.Message = "账户状态已更新，请重新连接"
		}
	}
}

func (s *Session) rejectRequest(event Notification) {
	s.logEvent(event, "server_request_rejected", "unsupported, invalid or stale native request; no authority granted")
	c := s.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.rpc.respond(ctx, event.RequestID, event.Sequence, nil, &NativeError{Code: -32601, Message: "Client method is not implemented"})
	}()
}
