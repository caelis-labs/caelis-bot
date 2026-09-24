package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

type prompt struct {
	id                         json.RawMessage
	sequence                   uint64
	method, thread, turn, item string
	view                       api.Approval
	choices                    map[string]any
	questions                  []nativeQuestion
	form                       *formSchema
	permissions                map[string]json.RawMessage
}
type nativeQuestion struct {
	ID       string `json:"id"`
	Header   string `json:"header"`
	Question string `json:"question"`
	IsOther  bool   `json:"isOther"`
	IsSecret bool   `json:"isSecret"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}
type formSchema struct {
	Type        string               `json:"type"`
	Schema      string               `json:"$schema"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Required    []string             `json:"required"`
	Properties  map[string]formField `json:"properties"`
}
type formField struct {
	Type        string          `json:"type"`
	Default     json.RawMessage `json:"default"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Enum        []string        `json:"enum"`
	MinLength   *int            `json:"minLength"`
	MaxLength   *int            `json:"maxLength"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
}

func pretty(v json.RawMessage) string {
	if len(v) == 0 || string(v) == "null" {
		return ""
	}
	var a any
	if json.Unmarshal(v, &a) != nil {
		return ""
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	return string(b)
}
func (s *Session) replacePrompt(id string, view api.Approval) {
	for i, p := range s.state.Approvals {
		if p.ID == id {
			s.state.Approvals[i] = view
			return
		}
	}
	s.state.Approvals = append(s.state.Approvals, view)
}
func (s *Session) addPrompt(event Notification) {
	var n struct {
		ThreadID    string                     `json:"threadId"`
		TurnID      string                     `json:"turnId"`
		ItemID      string                     `json:"itemId"`
		Reason      string                     `json:"reason"`
		Command     string                     `json:"command"`
		Cwd         string                     `json:"cwd"`
		GrantRoot   string                     `json:"grantRoot"`
		Network     json.RawMessage            `json:"networkApprovalContext"`
		Additional  json.RawMessage            `json:"additionalPermissions"`
		Permissions map[string]json.RawMessage `json:"permissions"`
		Decisions   []json.RawMessage          `json:"availableDecisions"`
		Questions   []nativeQuestion           `json:"questions"`
		Mode        string                     `json:"mode"`
		Message     string                     `json:"message"`
		ServerName  string                     `json:"serverName"`
		URL         string                     `json:"url"`
		Schema      json.RawMessage            `json:"requestedSchema"`
	}
	if err := json.Unmarshal(event.Params, &n); err != nil {
		s.logEvent(event, "server_request_decode_failed", diagnosticlog.DecodeReason(err))
		s.rejectRequest(event)
		return
	}
	if !s.ownsThread(n.ThreadID) || (n.ThreadID == s.binding.ThreadID && n.TurnID != "" && terminal(s.runs[n.TurnID])) || s.childTerminals[opaque(n.ThreadID, n.TurnID)] {
		s.rejectRequest(event)
		return
	}
	id := opaque(s.instance, fmt.Sprint(s.epoch), fmt.Sprint(event.Sequence), string(event.RequestID))
	p := &prompt{id: event.RequestID, sequence: event.Sequence, method: event.Method, thread: n.ThreadID, turn: n.TurnID, item: n.ItemID, choices: map[string]any{}}
	p.view = api.Approval{ID: id, Description: n.Reason, Status: "pending", Choices: []api.Choice{}, Questions: []api.Question{}}
	add := func(id, labelKey string, value any) {
		p.view.Choices = append(p.view.Choices, api.Choice{ID: id, LabelKey: labelKey})
		p.choices[id] = value
	}
	switch event.Method {
	case "item/commandExecution/requestApproval":
		p.view.TitleKey = "chat.approveCommand"
		p.view.Action = n.Command
		p.view.Target = n.Cwd

		if value := pretty(n.Network); value != "" {
			p.view.TitleKey = "chat.approveNetwork"
			p.view.Sections = append(p.view.Sections, api.ApprovalSection{TitleKey: "chat.networkTargets", Text: value})
		}
		if value := pretty(n.Additional); value != "" {
			p.view.Sections = append(p.view.Sections, api.ApprovalSection{TitleKey: "chat.additionalPermissions", Text: value})
		}
		decisions := n.Decisions
		if decisions == nil {
			decisions = []json.RawMessage{json.RawMessage(`"accept"`), json.RawMessage(`"acceptForSession"`), json.RawMessage(`"decline"`), json.RawMessage(`"cancel"`)}
		}
		for i, d := range decisions {
			var choice string
			_ = json.Unmarshal(d, &choice)
			detail := ""
			scope := map[string]string{"accept": "once", "acceptForSession": "conversation", "decline": "deny", "cancel": "deny"}[choice]
			label := map[string]string{"accept": "chat.allowOnce", "acceptForSession": "chat.allowConversation", "decline": "chat.decline", "cancel": "chat.cancelOperation"}[choice]
			if label == "" {
				var object map[string]json.RawMessage
				if json.Unmarshal(d, &object) != nil {
					continue
				}
				if _, ok := object["acceptWithExecpolicyAmendment"]; ok {
					label = "chat.allowCommandRule"
				} else if _, ok := object["applyNetworkPolicyAmendment"]; ok {
					label = "chat.applyNetworkRule"
				} else {
					continue
				}
				detail = pretty(d)
				scope = "rule"
			}
			add(fmt.Sprintf("decision-%d", i), label, map[string]any{"decision": d})
			p.view.Choices[len(p.view.Choices)-1].Scope = scope
			p.view.Choices[len(p.view.Choices)-1].Details = detail
		}
	case "item/fileChange/requestApproval":
		p.view.TitleKey = "chat.approveFiles"
		item := s.nativeItems[opaque(n.TurnID, n.ItemID)]
		for _, change := range item.Changes {
			p.view.Details += change.Path + "\n" + change.Diff + "\n"
		}
		if n.GrantRoot != "" {
			p.view.Sections = append(p.view.Sections, api.ApprovalSection{TitleKey: "chat.grantDirectory", Text: n.GrantRoot})
		}
		if p.view.Details != "" || n.GrantRoot != "" {
			add("accept", "chat.allowOnce", map[string]string{"decision": "accept"})
			add("acceptForSession", "chat.allowConversation", map[string]string{"decision": "acceptForSession"})
			p.view.Choices[len(p.view.Choices)-1].Scope = "conversation"
		}
		add("decline", "chat.decline", map[string]string{"decision": "decline"})
		add("cancel", "chat.cancelOperation", map[string]string{"decision": "cancel"})
	case "item/permissions/requestApproval":
		p.view.TitleKey = "chat.approvePermissions"
		p.view.Target = n.Cwd
		b, _ := json.Marshal(n.Permissions)
		p.view.Sections = append(p.view.Sections, api.ApprovalSection{TitleKey: "chat.turnPermissions", Text: pretty(b)})
		p.permissions = n.Permissions
		granted := map[string]json.RawMessage{}
		for _, key := range []string{"network", "fileSystem"} {
			if value, ok := n.Permissions[key]; ok && string(value) != "null" {
				granted[key] = value
			}
		}
		if len(granted) > 0 {
			add("allow", "chat.allowTurn", map[string]any{"permissions": granted, "scope": "turn"})
		}
		add("decline", "chat.decline", map[string]any{"permissions": map[string]any{}, "scope": "turn"})
	case "item/tool/requestUserInput":
		p.view.TitleKey = "chat.confirmationRequired"
		p.questions = n.Questions
		for _, q := range n.Questions {
			view := api.Question{ID: q.ID, Title: q.Question, Secret: q.IsSecret, Required: true, Type: "text", Options: []api.Choice{}}
			for _, o := range q.Options {
				view.Options = append(view.Options, api.Choice{ID: o.Label, Label: o.Label + " " + o.Description})
			}
			if len(q.Options) > 0 && !q.IsOther {
				view.Type = "select"
			}
			p.view.Questions = append(p.view.Questions, view)
		}
		add("answer", "chat.submitAnswer", nil)
	case "mcpServer/elicitation/request":
		p.view.Title = n.ServerName
		p.view.TitleKey = "chat.serverConfirmationRequired"
		p.view.Description = n.Message
		if n.Mode == "url" && safeWebURL(n.URL) {
			p.view.URL = n.URL
			add("accept", "chat.authorizationDone", map[string]any{"action": "accept", "content": nil, "_meta": nil})
		} else if n.Mode == "form" {
			var form formSchema
			decoder := json.NewDecoder(bytes.NewReader(n.Schema))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&form) == nil && form.Type == "object" {
				valid := true
				keys := make([]string, 0, len(form.Properties))
				for k := range form.Properties {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				for _, key := range keys {
					f := form.Properties[key]
					if !slices.Contains([]string{"string", "number", "integer", "boolean"}, f.Type) {
						valid = false
						break
					}
					q := api.Question{ID: key, Title: f.Title, Type: f.Type, Required: slices.Contains(form.Required, key), Options: []api.Choice{}}
					if q.Title == "" {
						q.Title = key
					}
					if f.Description != "" {
						q.Title += " — " + f.Description
					}
					for _, v := range f.Enum {
						q.Options = append(q.Options, api.Choice{ID: v, Label: v})
					}
					if len(f.Enum) > 0 {
						q.Type = "select"
					}
					p.view.Questions = append(p.view.Questions, q)
				}
				for _, required := range form.Required {
					if _, exists := form.Properties[required]; !exists {
						valid = false
					}
				}
				if valid {
					p.form = &form
					add("accept", "chat.submitApproval", nil)
				} else {
					p.view.Questions = nil

				}
			}
			if p.form == nil {
				p.view.Questions = nil
				p.view.NoticeKey = "chat.unsupportedApprovalForm"
			}
		}
		add("decline", "chat.decline", map[string]any{"action": "decline", "content": nil, "_meta": nil})
		add("cancel", "chat.cancelApproval", map[string]any{"action": "cancel", "content": nil, "_meta": nil})
	default:
		s.rejectRequest(event)
		s.state.Message = "后端请求了尚不支持的交互，已明确拒绝；任务结果请以后端回执为准。"
		return
	}
	if task := s.taskByThread(n.ThreadID); task != nil {
		p.view.TaskTitle = task.View.Title
	}
	s.prompts[id] = p
	s.promptHandles[string(event.RequestID)] = id
	s.replacePrompt(id, p.view)
	s.state.Phase = "attention"
}
func (s *Session) Decide(ctx context.Context, d api.Decision) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	p, ok := s.prompts[d.ID]
	if !ok || p.view.Status != "pending" || s.state.Connection != "ready" {
		s.mu.Unlock()
		return errors.New("这项请求已失效或已处理")
	}
	result, offered := p.choices[d.Choice]
	if !offered {
		s.mu.Unlock()
		return errors.New("请选择当前请求提供的选项")
	}
	var err error
	if d.Choice == "answer" {
		result, err = questionAnswers(p, d.Answers)
	} else if d.Choice == "accept" && p.form != nil {
		result, err = formAnswers(p.form, d.Answers)
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	p.view.Status = "sending"
	s.replacePrompt(d.ID, p.view)
	s.update()
	c := s.client
	s.mu.Unlock()
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	err = c.rpc.respond(ctx, p.id, p.sequence, result, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.prompts[d.ID]; exists && current == p {
		if err == nil {
			p.view.Status = "sent"
		} else {
			p.view.Status = "unknown"
			s.state.Message = "确认结果尚未收到，请重新连接核对；不要重复授权"
		}
		s.replacePrompt(d.ID, p.view)
		s.update()
	}
	if err != nil {
		return errors.New("该请求已失效，或确认结果尚未收到")
	}
	return nil
}
func questionAnswers(p *prompt, answers map[string][]string) (any, error) {
	result := map[string]any{}
	for _, q := range p.questions {
		a := answers[q.ID]
		if len(a) != 1 || strings.TrimSpace(a[0]) == "" {
			return nil, errors.New("请回答每个问题")
		}
		if len(q.Options) > 0 && !q.IsOther && !slices.ContainsFunc(q.Options, func(o struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		}) bool {
			return o.Label == a[0]
		}) {
			return nil, errors.New("请选择提供的答案")
		}
		result[q.ID] = map[string]any{"answers": a}
	}
	return map[string]any{"answers": result}, nil
}
func formAnswers(schema *formSchema, answers map[string][]string) (any, error) {
	content := map[string]any{}
	for key, f := range schema.Properties {
		a := answers[key]
		if len(a) == 0 || a[0] == "" {
			if slices.Contains(schema.Required, key) {
				return nil, errors.New("请填写必填项")
			}
			continue
		}
		v := a[0]
		if len(f.Enum) > 0 && !slices.Contains(f.Enum, v) {
			return nil, errors.New("选项无效")
		}
		switch f.Type {
		case "string":
			if (f.MinLength != nil && len([]rune(v)) < *f.MinLength) || (f.MaxLength != nil && len([]rune(v)) > *f.MaxLength) {
				return nil, errors.New("文本长度不符合要求")
			}
			content[key] = v
		case "boolean":
			if v != "true" && v != "false" {
				return nil, errors.New("请选择是或否")
			}
			content[key] = v == "true"
		case "number", "integer":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (f.Type == "integer" && n != float64(int64(n))) || (f.Minimum != nil && n < *f.Minimum) || (f.Maximum != nil && n > *f.Maximum) {
				return nil, errors.New("数字不符合要求")
			}
			content[key] = n
		}
	}
	return map[string]any{"action": "accept", "content": content, "_meta": nil}, nil
}
func safeWebURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func (s *Session) ApprovalURL(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok || p.view.Status != "pending" || !safeWebURL(p.view.URL) {
		return "", errors.New("授权链接已失效")
	}
	return p.view.URL, nil
}
