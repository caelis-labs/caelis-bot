// Package textchannel owns the text-only contract shared by companion channels.
// A transport authenticates its owner and persists its ingress before calling it.
package textchannel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Inbound and Outbound describe the minimum text capability. Native buttons are
// optional transport presentation and must submit the same api.Decision.
type Inbound struct{ Channel, Conversation, ID, Text string }
type Outbound struct{ Channel, Conversation, ID, Text string }
type Notice struct {
	ID, Text string
	Origin   Origin
}

type Origin struct{ Channel, Conversation string }
type option struct{ ID, Label, Scope, Details string }
type prompt struct {
	NativeID, TurnKey, Owner, QuestionID, Fingerprint string
	Options                                           []option
	Free                                              bool
}
type claim struct{ State, Feedback string }
type document struct {
	Version                    int
	NextApproval, NextQuestion uint64
	Origins                    map[string]Origin // exact accepted submission ID, saved before dispatch
	Prompts                    map[string]prompt // immutable short numbers; never recycled
	Claims                     map[string]claim  // native request ID, shared across transports
	Replies                    map[string]string // original channel message ID -> immutable feedback
	ReplyOrder                 []string
}
type Store struct {
	mu     sync.Mutex
	path   string
	state  document
	decide func(context.Context, api.Decision) error
}

func Open(root string, decide func(context.Context, api.Decision) error) (*Store, error) {
	s := &Store{path: filepath.Join(root, "text-channel-control.json"), decide: decide}
	s.state = document{Version: 1, Origins: map[string]Origin{}, Prompts: map[string]prompt{}, Claims: map[string]claim{}, Replies: map[string]string{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Origins == nil || s.state.Prompts == nil || s.state.Claims == nil || s.state.Replies == nil {
		return nil, errors.New("text channel control state unavailable")
	}
	if s.state.ReplyOrder == nil && len(s.state.Replies) > 0 {
		for key := range s.state.Replies {
			s.state.ReplyOrder = append(s.state.ReplyOrder, key)
		}
		slices.Sort(s.state.ReplyOrder)
	}
	return s, nil
}
func (s *Store) save() error { return localstate.Write(s.path, s.state) }

// RecordOrigin runs before native Submit. It prevents echoing the original
// user item to its own transport and retains the Weixin context token.
func (s *Store) RecordOrigin(id string, origin Origin) error {
	if id == "" || origin.Channel == "" || origin.Conversation == "" {
		return errors.New("invalid message origin")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.state.Origins[id]; ok {
		if previous != origin {
			return errors.New("message origin changed")
		}
		return nil
	}
	s.state.Origins[id] = origin
	if err := s.save(); err != nil {
		delete(s.state.Origins, id)
		return err
	}
	return nil
}
func (s *Store) OriginOf(id string) (Origin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Origins[id]
	return value, ok
}

// Route chooses a Weixin context token for delivery. Ambiguous steering falls
// back to that transport's current token; it never filters conversation output.
func (s *Store) Route(snapshot api.Snapshot, requestID, turnKey string) (Origin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if origin, ok := s.state.Origins[requestID]; ok {
		return origin, true
	}
	var result Origin
	found := false
	for _, item := range snapshot.Items {
		if (item.Kind != "user" && item.Kind != "hostNotice") || item.TurnKey == "" || item.TurnKey != turnKey {
			continue
		}
		origin := s.state.Origins[item.RequestID] // empty means desktop
		if found && result != origin {
			return Origin{}, false
		}
		result, found = origin, true
	}
	return result, found
}

// Card freezes the exact ordered native options when a short ID is first shown.
// A changed option list, target, owner or turn cannot reuse that short ID.
func (s *Store) Card(a api.Approval) (string, string, error) {
	if a.Status != "pending" || a.ID == "" {
		return "", "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []string
	if a.Title != "" {
		lines = append(lines, a.Title)
	}
	for _, section := range a.Sections {
		if section.Text != "" {
			lines = append(lines, section.Text)
		}
	}
	if a.Action != "" {
		lines = append(lines, a.Action)
	}
	if a.Target != "" {
		lines = append(lines, a.Target)
	}
	if a.Description != "" {
		lines = append(lines, a.Description)
	}
	if a.Details != "" {
		lines = append(lines, a.Details)
	}
	add := func(kind string, p prompt) (string, error) {
		encoded, _ := json.Marshal(p)
		h := sha256.Sum256(encoded)
		p.Fingerprint = hex.EncodeToString(h[:])
		for id, old := range s.state.Prompts {
			if old.NativeID == p.NativeID && old.QuestionID == p.QuestionID && old.Fingerprint == p.Fingerprint {
				return id, nil
			}
		}
		var id string
		if kind == "A" {
			s.state.NextApproval++
			id = fmt.Sprintf("A%d", s.state.NextApproval)
		} else {
			s.state.NextQuestion++
			id = fmt.Sprintf("Q%d", s.state.NextQuestion)
		}
		s.state.Prompts[id] = p
		if err := s.save(); err != nil {
			delete(s.state.Prompts, id)
			return "", err
		}
		return id, nil
	}
	if len(a.Questions) > 0 {
		if len(a.Questions) != 1 || a.Questions[0].Secret || a.Questions[0].Multiple {
			return strings.Join(lines, "\n") + "\n请在 Mac 上回答此请求。", "", nil
		}
		q := a.Questions[0]
		p := prompt{NativeID: a.ID, TurnKey: a.TurnKey, Owner: a.Owner, QuestionID: q.ID, Free: q.Type == "text"}
		for _, o := range q.Options {
			p.Options = append(p.Options, option{o.ID, label(o), o.Scope, o.Details})
		}
		id, err := add("Q", p)
		if err != nil {
			return "", "", err
		}
		lines = append(lines, q.Title)
		if decision := s.state.Claims[a.ID]; decision.State != "" {
			return strings.Join(append(lines, decision.Feedback), "\n"), id, nil
		}
		for index, o := range p.Options {
			lines = append(lines, fmt.Sprintf("/answer %s %d — %s", id, index+1, meaning(o)))
		}
		if p.Free {
			lines = append(lines, "/answer "+id+" <你的完整回答>")
		}
		return strings.Join(lines, "\n"), id, nil
	}
	p := prompt{NativeID: a.ID, TurnKey: a.TurnKey, Owner: a.Owner}
	for _, o := range a.Choices {
		p.Options = append(p.Options, option{o.ID, label(o), o.Scope, o.Details})
	}
	if len(p.Options) == 0 || a.URL != "" {
		return strings.Join(lines, "\n") + "\n请在 Mac 上处理此请求。", "", nil
	}
	id, err := add("A", p)
	if err != nil {
		return "", "", err
	}
	if decision := s.state.Claims[a.ID]; decision.State != "" {
		return strings.Join(append(lines, decision.Feedback), "\n"), id, nil
	}
	for index, o := range p.Options {
		lines = append(lines, fmt.Sprintf("/approve %s %d — %s", id, index+1, meaning(o)))
	}
	lines = append(lines, "如需修改计划，可回复 /approve "+id+" <意见>；意见不会授权执行。")
	return strings.Join(lines, "\n"), id, nil
}
func (s *Store) Claimed(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	decision, ok := s.state.Claims[id]
	return decision.Feedback, ok && decision.State != ""
}
func label(c api.Choice) string {
	if c.Label != "" {
		return c.Label
	}
	if c.LabelKey != "" {
		if value, ok := i18n.Lookup(i18n.Chinese, c.LabelKey, nil); ok {
			return value
		}
	}
	return "Runtime 选项 " + c.ID
}
func meaning(o option) string {
	value := o.Label
	if o.Scope != "" {
		scope := map[string]string{"once": "仅本次", "turn": "本轮", "session": "本会话", "conversation": "本次对话", "always": "始终", "rule": "规则"}[o.Scope]
		if scope == "" {
			scope = o.Scope
		}
		value += "（范围：" + scope + "）"
	}
	if o.Details != "" {
		value += "；" + o.Details
	}
	return value
}

func IsCommand(text string) bool { return strings.HasPrefix(strings.TrimSpace(text), "/") }

// Handle returns user-visible feedback for every command. A durable claim is
// written before Decide. Any uncertain result is terminal until the original
// Runtime receipt resolves; a duplicate message only returns saved feedback.
func (s *Store) Handle(ctx context.Context, in Inbound, snapshot api.Snapshot) string {
	key := in.Channel + "\x00" + in.Conversation + "\x00" + in.ID
	s.mu.Lock()
	if previous, ok := s.state.Replies[key]; ok {
		s.mu.Unlock()
		return previous
	}
	s.mu.Unlock()
	parts := strings.Fields(strings.TrimSpace(in.Text))
	if len(parts) == 0 {
		return "空命令。"
	}
	if parts[0] != "/approve" && parts[0] != "/answer" {
		return s.reply(key, "未知命令。请复制当前请求中的完整 /approve 或 /answer 命令。")
	}
	if len(parts) < 3 {
		return s.reply(key, "命令不完整。请复制当前请求中的完整命令。")
	}
	id := parts[1]
	if parts[0] == "/approve" && !strings.HasPrefix(id, "A") || parts[0] == "/answer" && !strings.HasPrefix(id, "Q") {
		return s.reply(key, "请求编号无效。")
	}
	s.mu.Lock()
	p, ok := s.state.Prompts[id]
	prior := s.state.Claims[p.NativeID]
	s.mu.Unlock()
	if !ok {
		return s.reply(key, "请求编号不存在或已经失效。")
	}
	if prior.State != "" {
		for _, a := range snapshot.Approvals {
			if a.ID == p.NativeID && a.Status == "resolved" {
				return s.reply(key, "原请求已处理。"+resolutionText(a.Resolution))
			}
		}
		return s.reply(key, prior.Feedback)
	}
	var current *api.Approval
	for i := range snapshot.Approvals {
		if snapshot.Approvals[i].ID == p.NativeID {
			current = &snapshot.Approvals[i]
			break
		}
	}
	if current == nil || current.Status != "pending" || current.TurnKey != p.TurnKey || current.Owner != p.Owner {
		return s.reply(key, "原请求已失效或已处理，请查看最新请求。")
	}
	// Check the immutable catalog against a fresh native snapshot before using an index.
	var actual []option
	if p.QuestionID != "" {
		if len(current.Questions) != 1 || current.Questions[0].ID != p.QuestionID {
			return s.reply(key, "问题已变化，请查看最新请求。")
		}
		for _, o := range current.Questions[0].Options {
			actual = append(actual, option{o.ID, label(o), o.Scope, o.Details})
		}
	} else {
		for _, o := range current.Choices {
			actual = append(actual, option{o.ID, label(o), o.Scope, o.Details})
		}
	}
	if encodedA, _ := json.Marshal(actual); string(encodedA) != marshalOptions(p.Options) {
		return s.reply(key, "选项已变化，请使用新请求中的命令。")
	}
	// Keep the whole tail after the short ID. Only an all-digit tail is an index.
	tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Text), parts[0]))
	tail = strings.TrimSpace(strings.TrimPrefix(tail, id))
	decision := api.Decision{ID: p.NativeID}
	if allDigits(tail) {
		n, err := strconv.Atoi(tail)
		if err != nil || n < 1 || n > len(p.Options) {
			return s.reply(key, "选项序号无效，请复制此请求展示的完整命令。")
		}
		if p.QuestionID == "" {
			decision.Choice = p.Options[n-1].ID
		} else {
			decision.Choice = "answer"
			decision.Answers = map[string][]string{p.QuestionID: {p.Options[n-1].ID}}
		}
	} else if p.QuestionID != "" && p.Free {
		if tail == "" {
			return s.reply(key, "回答不能为空。")
		}
		decision.Choice = "answer"
		decision.Answers = map[string][]string{p.QuestionID: {tail}}
	} else if p.QuestionID == "" {
		return s.reply(key, "已收到意见，但没有授权执行。请先拒绝当前审批，再让 Bot 按意见调整计划。")
	} else {
		return s.reply(key, "此问题只接受列出的选项，请复制对应命令。")
	}
	s.mu.Lock()
	if prior = s.state.Claims[p.NativeID]; prior.State != "" {
		s.mu.Unlock()
		return s.reply(key, prior.Feedback)
	}
	s.state.Claims[p.NativeID] = claim{State: "unknown", Feedback: "决定已收到，原请求结果待核对；不会重复提交。"}
	if err := s.save(); err != nil {
		delete(s.state.Claims, p.NativeID)
		s.mu.Unlock()
		return s.reply(key, "本地记录不可用，决定未提交。")
	}
	s.mu.Unlock()
	err := s.decide(ctx, decision)
	feedback := "决定已提交，等待 Runtime 确认。"
	state := "submitted"
	if err != nil {
		feedback = "决定结果暂不确定，请核对原请求；不会重复提交。"
		state = "unknown"
	}
	s.mu.Lock()
	s.state.Claims[p.NativeID] = claim{State: state, Feedback: feedback}
	_ = s.save()
	s.mu.Unlock()
	return s.reply(key, feedback)
}
func marshalOptions(v []option) string { b, _ := json.Marshal(v); return string(b) }
func resolutionText(r *api.ApprovalResolution) string {
	if r == nil {
		return ""
	}
	switch r.Outcome {
	case "allowed":
		return "结果：已允许。"
	case "declined":
		return "结果：已拒绝。"
	case "cancelled":
		return "结果：已取消。"
	default:
		return ""
	}
}
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func (s *Store) reply(key, body string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.state.Replies[key]; !exists {
		s.state.ReplyOrder = append(s.state.ReplyOrder, key)
	}
	s.state.Replies[key] = body
	_ = s.save()
	return body
}
func (s *Store) Publish(in Inbound, body string) string {
	if in.Channel == "" || in.ID == "" {
		return ""
	}
	return s.reply(in.Channel+"\x00"+in.Conversation+"\x00"+in.ID, body)
}
func (s *Store) Notices() []Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Notice, 0, len(s.state.Replies))
	for _, key := range s.state.ReplyOrder {
		body, exists := s.state.Replies[key]
		if !exists {
			continue
		}
		h := sha256.Sum256([]byte(key))
		parts := strings.SplitN(key, "\x00", 3)
		origin := Origin{}
		if len(parts) == 3 {
			origin = Origin{Channel: parts[0], Conversation: parts[1]}
		}
		result = append(result, Notice{ID: hex.EncodeToString(h[:]), Text: body, Origin: origin})
	}
	return result
}
func (s *Store) Revision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.state.ReplyOrder))
}
